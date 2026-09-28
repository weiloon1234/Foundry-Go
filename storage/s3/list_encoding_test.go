package s3_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func TestProviderListingPreservesSpacesPlusAndLiteralEscapes(t *testing.T) {
	for _, provider := range []s3.Provider{s3.AWS, s3.R2} {
		for _, kind := range []string{"objects", "uploads"} {
			t.Run(fmt.Sprintf("%d/%s", provider, kind), func(t *testing.T) {
				keys := []string{"page/a +%2f?#é.txt", "page/b +%20.txt"}
				encode := url.QueryEscape
				if provider == s3.R2 {
					encode = url.PathEscape
				}
				lists := 0
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "HEAD" {
						if r.URL.Path != "/fixture-bucket/isolated/"+keys[lists-1] || r.Header.Get("If-Match") != `"one"` {
							t.Error("listing HEAD changed object identity")
						}
						wireHeaders(w, 1, `"one"`)
						return
					}
					q := r.URL.Query()
					if q.Get("prefix") != "isolated/page/" || q.Get("encoding-type") != "url" || lists > 1 {
						t.Error("listing scope or bounds changed")
						w.WriteHeader(400)
						return
					}
					if lists == 1 {
						if kind == "uploads" {
							if q.Get("key-marker") != "isolated/"+keys[0] || q.Get("upload-id-marker") != "upload+%/" {
								t.Error("upload cursor changed identity")
							}
						} else if q.Get("continuation-token") != "cursor+%/" {
							t.Error("opaque object cursor changed")
						}
					}
					key := encode("isolated/" + keys[lists])
					truncated := lists == 0
					lists++
					w.Header().Set("Content-Type", "application/xml")
					if kind == "uploads" {
						fmt.Fprintf(w, `<ListMultipartUploadsResult><EncodingType>url</EncodingType><IsTruncated>%t</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextUploadIdMarker>upload+%%/</NextUploadIdMarker><Upload><Key>%s</Key><UploadId>upload+%%/</UploadId><Initiated>2023-11-14T22:13:20Z</Initiated></Upload></ListMultipartUploadsResult>`, truncated, key, key)
					} else {
						fmt.Fprintf(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>%t</IsTruncated><NextContinuationToken>cursor+%%/</NextContinuationToken><Contents><Key>%s</Key><ETag>"one"</ETag><Size>1</Size><LastModified>2023-11-14T22:13:20Z</LastModified></Contents></ListBucketResult>`, truncated, key)
					}
				})
				configure := func(c *s3.Config) { c.Namespace, _ = storage.ParsePrefix("isolated/") }
				var disk *storage.Disk
				var backend *s3.Backend
				if provider == s3.AWS {
					disk, backend = wireDisk(t, handler, configure)
				} else {
					disk, backend = r2WireBackend(t, handler, configure)
				}
				prefix, _ := storage.ParsePrefix("page/")
				options := storage.ListOptions{Prefix: prefix, Limit: 1}
				for i, expected := range keys {
					if kind == "uploads" {
						page, err := backend.ListUploads(t.Context(), options)
						if err != nil || len(page.Uploads) != 1 || page.Uploads[0].Reference.Key().String() != expected {
							t.Fatal("multipart listing lost exact key", err)
						}
						options.Cursor = page.Next
					} else {
						page, err := disk.List(t.Context(), options)
						if err != nil || len(page.Objects) != 1 || page.Objects[0].Key.String() != expected {
							t.Fatal("object listing lost exact key", err)
						}
						options.Cursor = page.Next
					}
					if options.Cursor.IsZero() != (i == 1) {
						t.Fatal("pagination did not advance")
					}
				}
			})
		}
	}
}
