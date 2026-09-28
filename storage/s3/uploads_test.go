package s3_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func TestScopedMultipartInspectionAndExplicitAbort(t *testing.T) {
	var aborts, inspections atomic.Int32
	_, backend := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("uploads") {
			if q.Get("prefix") != "isolated/staging/" || q.Get("max-uploads") != "2" {
				t.Error("upload listing escaped scope")
			}
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<ListMultipartUploadsResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated><Upload><Key>%s</Key><UploadId>orphan-id</UploadId><Initiated>2023-11-14T22:13:20Z</Initiated></Upload></ListMultipartUploadsResult>`, url.QueryEscape("isolated/staging/a +%2f.txt"))
			return
		}
		if r.URL.Path != "/fixture-bucket/isolated/staging/a +%2f.txt" || q.Get("uploadId") != "orphan-id" {
			t.Error("abort changed identity")
		}
		if r.Method == "DELETE" {
			aborts.Add(1)
			w.WriteHeader(204)
			return
		}
		if r.Method == "GET" {
			if inspections.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/xml")
				io.WriteString(w, `<ListPartsResult><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><ETag>"part"</ETag><Size>1</Size></Part></ListPartsResult>`)
			} else {
				wireError(w, 404, "NoSuchUpload")
			}
			return
		}
		t.Error("unexpected object mutation")
	}), func(c *s3.Config) { c.Namespace, _ = storage.ParsePrefix("isolated/") })
	prefix, _ := storage.ParsePrefix("staging/")
	page, err := backend.ListUploads(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 2})
	if err != nil || len(page.Uploads) != 1 || page.Uploads[0].Reference.Key().String() != "staging/a +%2f.txt" {
		t.Fatal("upload metadata changed", err)
	}
	if err := backend.AbortUpload(t.Context(), page.Uploads[0].Reference); err != nil || aborts.Load() != 2 || inspections.Load() != 2 {
		t.Fatal("late parts were not reconciled", err)
	}
	if err := backend.AbortUpload(t.Context(), s3.UploadReference{}); !errors.Is(err, storage.Invalid) {
		t.Fatal("zero upload reference accepted", err)
	}
}
func TestAbortFailureReferenceCanBeRecoveredOnlyWithinItsScope(t *testing.T) {
	peer := &uploadPeer{failAbort: true}
	disk, backend := wireDisk(t, peer, nil)
	source := io.MultiReader(&zeroSource{remaining: s3.MinPartBytes + 1}, failedSource{})
	_, err := disk.Put(t.Context(), objectKey(t), source, storage.PutOptions{})
	var failure *storage.Error
	if !errors.As(err, &failure) {
		t.Fatal(err)
	}
	cleanup, ok := failure.Cleanup().Get()
	if !ok {
		t.Fatal("missing cleanup reference")
	}
	reference, err := backend.UploadFromCleanup(cleanup)
	if err != nil || reference.Key() != objectKey(t) {
		t.Fatal("cannot recover own cleanup", err)
	}
	otherConfig := s3.DefaultConfig("other-bucket", "us-east-1")
	other, err := s3.Prepare(otherConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.UploadFromCleanup(cleanup); !errors.Is(err, storage.Invalid) {
		t.Fatal("foreign cleanup reference accepted", err)
	}
	peer.mu.Lock()
	peer.failAbort = false
	peer.mu.Unlock()
	if err := backend.AbortUpload(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
}
