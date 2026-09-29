package s3_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func listedObject(key, tag string, size int) string {
	return fmt.Sprintf(`<Contents><Key>%s</Key><ETag>%s</ETag><Size>%d</Size><LastModified>2023-11-14T22:13:20Z</LastModified></Contents>`, key, tag, size)
}

func TestListingBuildsEntriesWithoutPerObjectRequestsAndSkipsForeignEntries(t *testing.T) {
	var lists, others atomic.Int32
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("list-type") != "2" {
			others.Add(1)
			wireError(w, 403, "AccessDenied")
			return
		}
		lists.Add(1)
		if r.URL.Query().Has("delimiter") {
			t.Error("recursive listing sent a delimiter")
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated>`+
			listedObject(url.QueryEscape("isolated/page/a.txt"), `"one"`, 1)+
			listedObject(url.QueryEscape("isolated/page/b.txt"), `"two"`, 11)+
			listedObject("isolated/page/%ZZ", `"three"`, 1)+
			listedObject(url.QueryEscape("isolated/page/c/../d"), `"four"`, 1)+
			listedObject(url.QueryEscape("isolated/page/e.txt"), ``, 1)+
			listedObject(url.QueryEscape("isolated/page/f.txt"), `"six"`, 2)+
			`</ListBucketResult>`)
	}), func(c *s3.Config) {
		c.Namespace, _ = storage.ParsePrefix("isolated/")
		c.MaxObjectBytes = 10
	})
	prefix, _ := storage.ParsePrefix("page/")
	page, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 10})
	if err != nil {
		t.Fatal("one unrepresentable entry failed the page", err)
	}
	if len(page.Objects) != 2 || page.Objects[0].Key.String() != "page/a.txt" || page.Objects[1].Key.String() != "page/f.txt" || page.Skipped != 4 {
		t.Fatal("listing did not skip foreign, oversized, unparsable or unvalidated entries", page)
	}
	first := page.Objects[0]
	if first.Size != 1 || first.ETag != `"one"` || first.Modified.IsZero() || first.ContentType != "" || first.Checksum.IsSet() {
		t.Fatal("listing metadata was not taken from the listing", first)
	}
	if lists.Load() != 1 || others.Load() != 0 {
		t.Fatal("listing issued per-object requests", lists.Load(), others.Load())
	}
}

func TestDelimitedListingReportsCommonPrefixesAsDirectories(t *testing.T) {
	var calls atomic.Int32
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if r.Method != "GET" || q.Get("delimiter") != "/" || q.Get("prefix") != "isolated/page/" {
			t.Error("delimited listing request changed", r.Method, q)
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated>`+
			listedObject(url.QueryEscape("isolated/page/a.txt"), `"one"`, 1)+
			`<CommonPrefixes><Prefix>`+url.QueryEscape("isolated/page/sub dir/")+`</Prefix></CommonPrefixes>`+
			`<CommonPrefixes><Prefix>`+url.QueryEscape("isolated/page/z/")+`</Prefix></CommonPrefixes>`+
			`</ListBucketResult>`)
	}), func(c *s3.Config) { c.Namespace, _ = storage.ParsePrefix("isolated/") })
	prefix, _ := storage.ParsePrefix("page/")
	page, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 3, Delimited: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Key.String() != "page/a.txt" || len(page.Directories) != 2 || page.Directories[0].String() != "page/sub dir/" || page.Directories[1].String() != "page/z/" {
		t.Fatal("common prefixes were not reported as directories", page)
	}
	// A recursive listing must never receive child prefixes.
	before := calls.Load()
	if _, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 1, Delimited: true}); !errors.Is(err, storage.IntegrityFailed) {
		t.Fatal("provider exceeded the combined object/directory limit", err)
	}
	if calls.Load() != before+1 {
		t.Fatal("bounded listing retried")
	}
}

func TestRecursiveListingRejectsUnexpectedCommonPrefixes(t *testing.T) {
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated><CommonPrefixes><Prefix>page/sub/</Prefix></CommonPrefixes></ListBucketResult>`)
	}), nil)
	if _, err := disk.List(t.Context(), storage.ListOptions{Limit: 5}); !errors.Is(err, storage.IntegrityFailed) {
		t.Fatal("recursive listing accepted provider directories", err)
	}
}
