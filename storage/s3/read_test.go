package s3_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
	"github.com/weiloon1234/Foundry-Go/value"
)

func wireError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code><Message>private endpoint detail</Message></Error>", code)
}
func wireHeaders(w http.ResponseWriter, size int, tag string) {
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("ETag", tag)
	w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
}
func TestSDKGetPreservesEncodedKeyAndRangeMetadata(t *testing.T) {
	key, err := storage.ParseKey("folder/a +%2f?#.txt")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/fixture-bucket/isolated/"+key.String() || r.Header.Get("If-Match") != `"one"` || r.Header.Get("Range") != "bytes=7-105" {
			t.Error("SDK changed request identity or options")
		}
		wireHeaders(w, 3, `"one"`)
		w.Header().Set("Content-Range", "bytes 7-9/10")
		w.WriteHeader(206)
		io.WriteString(w, "hij")
	}), func(c *s3.Config) { c.Namespace, _ = storage.ParsePrefix("isolated/") })
	body, info, err := disk.Open(t.Context(), key, storage.ReadOptions{IfMatch: `"one"`, Range: value.Set(storage.ByteRange{Offset: 7, Length: 99})})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	cleanup := body.Close()
	if err != nil || cleanup != nil || string(data) != "hij" || info.Object.Size != 10 || info.Offset != 7 || info.Length != 3 || requests.Load() != 1 || disk.Stats().Active != 0 {
		t.Fatal("range representation changed", err, cleanup)
	}
}
func TestSDKRejectsWrongRangesAndFullChecksum(t *testing.T) {
	for _, span := range []string{"", "bytes 0-2/10", "bytes 7-11/10", "bytes 7-9/10"} {
		t.Run(span, func(t *testing.T) {
			disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wireHeaders(w, 3, `"one"`)
				if span != "" {
					w.Header().Set("Content-Range", span)
				}
				io.WriteString(w, "hij")
			}), nil)
			options := storage.ReadOptions{Range: value.Set(storage.ByteRange{Offset: 7, Length: 99})}
			body, _, err := disk.Open(t.Context(), objectKey(t), options)
			if span == "bytes 7-9/10" {
				if err != nil {
					t.Fatal(err)
				}
				_ = body.Close()
			} else if err == nil || body != nil {
				t.Fatal("malformed range accepted")
			}
			if disk.Stats().Active != 0 {
				t.Fatal("invalid representation retained capacity")
			}
		})
	}
	digest := storage.SHA256(sha256.Sum256([]byte("expected")))
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wireHeaders(w, 8, `"one"`)
		w.Header().Set("X-Amz-Meta-Foundry-Sha256", digest.String())
		io.WriteString(w, "tampered")
	}), nil)
	body, _, err := disk.Open(t.Context(), objectKey(t), storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(body)
	_ = body.Close()
	if !errors.Is(err, storage.IntegrityFailed) {
		t.Fatal("full checksum corruption accepted", err)
	}
}
func TestSDKDistinguishesAccessMissingBucketAndAmbiguousDelete(t *testing.T) {
	for _, tc := range []struct {
		method, code string
		status       int
		category     storage.Code
		outcome      storage.Outcome
	}{
		{"HEAD", "AccessDenied", 403, storage.Forbidden, storage.NotApplicable},
		{"GET", "NoSuchBucket", 404, storage.Unavailable, storage.NotApplicable},
		{"DELETE", "NoSuchKey", 404, storage.PreconditionFailed, storage.Unchanged},
		{"DELETE", "InternalError", 500, storage.Unavailable, storage.Unknown},
	} {
		t.Run(tc.method+tc.code, func(t *testing.T) {
			var calls atomic.Int32
			disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != tc.method {
					t.Error("wrong operation")
				}
				wireError(w, tc.status, tc.code)
			}), nil)
			var err error
			switch tc.method {
			case "HEAD":
				_, err = disk.Exists(t.Context(), objectKey(t))
			case "GET":
				_, _, err = disk.Open(t.Context(), objectKey(t), storage.ReadOptions{})
			case "DELETE":
				err = disk.Delete(t.Context(), objectKey(t), storage.DeleteOptions{IfMatch: `"one"`})
			}
			var failure *storage.Error
			if !errors.As(err, &failure) || failure.Code() != tc.category || failure.Outcome() != tc.outcome {
				t.Fatal("provider error classification", err)
			}
			if calls.Load() != 1 || strings.Contains(fmt.Sprintf("%+v", err), "private endpoint") {
				t.Fatal("unsafe retry or diagnostic disclosure")
			}
		})
	}
}
func TestSDKPaginationDecodesOnceAndPinsHeads(t *testing.T) {
	var calls atomic.Int32
	prefix, _ := storage.ParsePrefix("page/")
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method == "HEAD" {
			if r.Header.Get("If-Match") != `"one"` {
				t.Error("list HEAD not pinned")
			}
			wireHeaders(w, 1, `"one"`)
			return
		}
		if r.URL.Query().Get("prefix") != "isolated/page/" || r.URL.Query().Get("max-keys") != "1" || r.URL.Query().Get("encoding-type") != "url" {
			t.Error("listing scope/bounds changed")
		}
		key, truncated, next := "isolated/page/a%2f", true, "cursor+%/"
		if r.URL.Query().Get("continuation-token") == next {
			key, truncated, next = "isolated/page/b +.txt", false, ""
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>%t</IsTruncated><NextContinuationToken>%s</NextContinuationToken><Contents><Key>%s</Key><ETag>"one"</ETag><Size>1</Size><LastModified>2023-11-14T22:13:20Z</LastModified></Contents></ListBucketResult>`, truncated, next, url.QueryEscape(key))
	}), func(c *s3.Config) { c.Namespace, _ = storage.ParsePrefix("isolated/") })
	first, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 1})
	if err != nil || len(first.Objects) != 1 || first.Objects[0].Key.String() != "page/a%2f" || first.Next.IsZero() {
		t.Fatal("first page changed", err)
	}
	before := calls.Load()
	if _, err := disk.List(t.Context(), storage.ListOptions{Limit: 1, Cursor: first.Next}); !errors.Is(err, storage.Invalid) || calls.Load() != before {
		t.Fatal("cursor escaped prefix before provider call", err)
	}
	second, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 1, Cursor: first.Next})
	if err != nil || len(second.Objects) != 1 || second.Objects[0].Key.String() != "page/b +.txt" || !second.Next.IsZero() {
		t.Fatal("continuation changed", err)
	}
}

func TestSDKMalformedObjectMetadataIsAnIntegrityFailure(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, field := range []string{"ETag", "Content-Type", "X-Amz-Meta-Foundry-Sha256"} {
			t.Run(method+"/"+field, func(t *testing.T) {
				disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					wireHeaders(w, 0, `"one"`)
					w.Header().Set(field, "malformed")
				}), nil)
				var err error
				if method == "HEAD" {
					_, err = disk.Stat(t.Context(), objectKey(t), storage.ReadOptions{})
				} else {
					var body io.ReadCloser
					body, _, err = disk.Open(t.Context(), objectKey(t), storage.ReadOptions{})
					if body != nil {
						_ = body.Close()
						t.Error("malformed metadata returned a reader")
					}
				}
				var failure *storage.Error
				if !errors.As(err, &failure) || failure.Code() != storage.IntegrityFailed || failure.Outcome() != storage.NotApplicable {
					t.Fatal("provider metadata was mistaken for invalid application input", err)
				}
				if disk.Stats().Active != 0 {
					t.Fatal("malformed metadata retained reader capacity")
				}
			})
		}
	}
}
