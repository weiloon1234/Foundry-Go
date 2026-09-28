package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestETagTypedFilesPreserveConditionsAndBodyOwnership(t *testing.T) {
	payload := "typed file payload"
	for _, kind := range []string{"download", "manual-download", "known-stream", "unknown-stream"} {
		t.Run(kind, func(t *testing.T) {
			var closes, reads atomic.Int32
			var router *Router
			path := "/stream"
			if strings.Contains(kind, "download") {
				path = "/file"
				router = downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
					result := downloadText(payload, &closes)
					if kind == "download" {
						result = result.WithEntityTag("")
					}
					return result, nil
				})
			} else {
				router = streamRouter(t, streamEndpoint(), func(context.Context) (StreamContent, error) {
					reader := strings.NewReader(payload)
					content := StreamContent{Body: fileReaderCallbacks{
						read:  func(data []byte) (int, error) { reads.Add(1); return reader.Read(data) },
						close: func() error { closes.Add(1); return nil },
					}, Name: "export.txt", MediaType: "text/plain; charset=utf-8"}
					if kind == "known-stream" {
						content.Length = value.Set(int64(len(payload)))
					}
					return content, nil
				})
			}
			handler := etagTestHandler(t, DefaultETagConfig(), router.ServeHTTP)
			call := func(method, condition, tag string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(method, path, nil)
				if condition != "" {
					request.Header.Set(condition, tag)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				return response
			}
			first := call("GET", "", "")
			tag := first.Header().Get("ETag")
			if first.Code != 200 || first.Body.String() != payload || tag == "" || closes.Load() != 1 {
				t.Fatal("file body/validator/ownership", first.Code, first.Header(), closes.Load())
			}
			if first.Header().Get("Content-Disposition") == "" {
				t.Fatal("file presentation lost")
			}
			matched := call("GET", "If-None-Match", tag)
			if matched.Code != 304 || matched.Body.Len() != 0 || closes.Load() != 2 {
				t.Fatal("file condition or source cleanup changed", matched.Code, closes.Load())
			}
			before := reads.Load()
			head := call("HEAD", "", "")
			if head.Code != 200 || head.Body.Len() != 0 || closes.Load() != 3 || reads.Load() != before {
				t.Fatal("HEAD consumed stream data or lost cleanup")
			}
			if kind == "manual-download" && head.Header().Get("ETag") != "\"version-1\"" {
				t.Fatal("manual validator lost on HEAD")
			}
			ranged := call("GET", "Range", "bytes=0-4")
			if strings.Contains(kind, "download") {
				if ranged.Code != 206 || ranged.Body.String() != payload[:5] {
					t.Fatal("native download range changed", ranged.Code, ranged.Body.String())
				}
			} else if ranged.Code != 200 || ranged.Body.String() != payload || ranged.Header().Get("ETag") != "" {
				t.Fatal("unseekable range bypass changed")
			}
			if closes.Load() != 4 {
				t.Fatal("range source was not closed")
			}
		})
	}
}

func TestETagStaticAndSPAResponsesRetainNavigationPolicy(t *testing.T) {
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(assetFixture())))
	base, err := NewRouter(assets.Mount("etag.assets", "/assets").Register())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultSPAConfig()
	config.Exclude = []string{"/api", "/assets"}
	router, err := base.WithSPA("etag.spa", assets, config)
	if err != nil {
		t.Fatal(err)
	}
	handler := etagTestHandler(t, DefaultETagConfig(), router.ServeHTTP)
	call := func(path, tag string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", path, nil)
		request.Header.Set("Accept", "text/html")
		if tag != "" {
			request.Header.Set("If-None-Match", tag)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, path := range []string{"/assets/site.css", "/dashboard"} {
		first := call(path, "")
		tag := first.Header().Get("ETag")
		if first.Code != 200 || tag == "" || first.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("asset validator/policy", path, first.Header())
		}
		matched := call(path, "W/"+tag)
		if matched.Code != 304 || matched.Body.Len() != 0 || matched.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("asset revalidation changed policy")
		}
		if path == "/dashboard" {
			assertCORSVary(t, matched.Result().Header, "accept")
		}
	}
	if denied := call("/api/missing", "*"); denied.Code != 404 || denied.Header().Get("ETag") != "" {
		t.Fatal("SPA exclusion became conditional success")
	}
}
