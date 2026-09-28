package s3

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestAWSVersionCleanupPreservesEncodedKeysAndMarkers(t *testing.T) {
	keys := []string{"owned/a +%2f?#é.txt", "owned/b +%20.txt"}
	lists, deletes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method == "DELETE" {
			if deletes >= len(keys) || r.URL.Path != "/fixture-bucket/"+keys[deletes] || q.Get("versionId") != fmt.Sprint("version+%/", deletes) {
				t.Error("cleanup changed key or version identity")
				w.WriteHeader(400)
				return
			}
			deletes++
			w.WriteHeader(204)
			return
		}
		if r.Method != "GET" || !q.Has("versions") || q.Get("prefix") != "owned/" {
			t.Error("cleanup escaped namespace")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		if lists == 2 {
			if deletes != 2 {
				t.Error("verification preceded cleanup")
			}
			fmt.Fprint(w, `<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`)
			return
		}
		if lists == 1 && (q.Get("key-marker") != keys[0] || q.Get("version-id-marker") != "version+%/0") {
			t.Error("version cursor changed")
		}
		fmt.Fprintf(w, `<ListVersionsResult><EncodingType>url</EncodingType><IsTruncated>%t</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextVersionIdMarker>version+%%/0</NextVersionIdMarker><Version><Key>%s</Key><VersionId>version+%%/%d</VersionId></Version></ListVersionsResult>`, lists == 0, url.QueryEscape(keys[lists]), url.QueryEscape(keys[lists]), lists)
		lists++
	}))
	defer server.Close()
	cfg := DefaultConfig("fixture-bucket", "us-east-1")
	cfg.Namespace, _ = storage.ParsePrefix("owned/")
	client := awss3.NewFromConfig(aws.Config{Region: cfg.Region, Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}, func(o *awss3.Options) { o.BaseEndpoint = aws.String(server.URL); o.UsePathStyle = true })
	backend := &Backend{config: cfg, client: client}
	if err := cleanupAWSVersions(t.Context(), backend); err != nil || lists != 2 || deletes != 2 {
		t.Fatal("version cleanup failed", err)
	}
}
