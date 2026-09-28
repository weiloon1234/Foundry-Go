package http

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCompressionPreservesTypedFileBodiesAndOwnership(t *testing.T) {
	payload := strings.Repeat("typed file payload with unicode 中文\n", 2048)
	for _, coding := range []string{"gzip", "br"} {
		for _, kind := range []string{"download", "known-stream", "unknown-stream"} {
			for _, method := range []string{"GET", "HEAD"} {
				t.Run(coding+"/"+kind+"/"+method, func(t *testing.T) {
					var closes, reads atomic.Int32
					var router *Router
					if kind == "download" {
						router = downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
							return downloadText(payload, &closes), nil
						})
					} else {
						router = streamRouter(t, streamEndpoint(), func(context.Context) (StreamContent, error) {
							reader := strings.NewReader(payload)
							content := StreamContent{Body: fileReaderCallbacks{
								read:  func(p []byte) (int, error) { reads.Add(1); return reader.Read(p) },
								close: func() error { closes.Add(1); return nil },
							}, Name: "export.txt", MediaType: "text/plain; charset=utf-8"}
							if kind == "known-stream" {
								content.Length = value.Set(int64(len(payload)))
							}
							return content, nil
						})
					}
					handler := compressionTestHandler(t, DefaultCompressionConfig(), router.ServeHTTP)
					path := "/stream"
					if kind == "download" {
						path = "/file"
					}
					request := httptest.NewRequest(method, path, nil)
					request.Header.Set("Accept-Encoding", coding)
					recorder := httptest.NewRecorder()
					handler.ServeHTTP(recorder, request)
					header := recorder.Result().Header
					if recorder.Code != 200 || closes.Load() != 1 || header.Get("Content-Disposition") == "" {
						t.Fatal("typed file status/ownership", recorder.Code, closes.Load(), header)
					}
					if method == "HEAD" {
						if recorder.Body.Len() != 0 || reads.Load() != 0 {
							t.Fatal("HEAD consumed a stream")
						}
						if header.Get("Content-Length") != "" {
							t.Fatal("HEAD retained identity length for negotiated response")
						}
						return
					}
					if header.Get("Content-Encoding") != coding || header.Get("Content-Length") != "" {
						t.Fatal("file representation metadata", header)
					}
					if got := string(decodeCompressed(t, coding, recorder.Body.Bytes())); got != payload {
						t.Fatal("compression changed typed file payload")
					}
					if kind == "download" && header.Get("Etag") != "W/\"version-1\"" {
						t.Fatal("compressed download retained a strong identity validator", header)
					}
				})
			}
		}
	}
}

func TestCompressionPreservesAssetRangesAndSPAFallback(t *testing.T) {
	shell := strings.Repeat("<main>typed SPA shell</main>", 128)
	css := strings.Repeat("body { color: black; }\n", 128)
	source := assetFixture()
	source["index.html"] = &fstest.MapFile{Data: []byte(shell)}
	source["site.css"] = &fstest.MapFile{Data: []byte(css), ModTime: source["site.css"].ModTime}
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(source)))
	base, err := NewRouter(assets.Mount("public.assets", "/assets").Register())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultSPAConfig()
	config.Exclude = []string{"/api", "/assets"}
	router, err := base.WithSPA("public.spa", assets, config)
	if err != nil {
		t.Fatal(err)
	}
	for _, coding := range []string{"gzip", "br"} {
		handler := compressionTestHandler(t, DefaultCompressionConfig(), router.ServeHTTP)
		for _, tc := range []struct {
			name, path, header, argument string
			status                       int
			body                         string
			compressed, navigation       bool
		}{
			{"asset", "/assets/site.css", "", "", 200, css, true, false},
			{"range", "/assets/site.css", "Range", "bytes=0-19", 206, css[:20], false, false},
			{"condition", "/assets/site.css", "If-Modified-Since", "Thu, 02 Jan 2020 03:04:05 GMT", 304, "", false, false},
			{"navigation", "/dashboard", "Accept", "text/html", 200, shell, true, true},
			{"missing-asset", "/assets/missing.js", "Accept", "text/html", 404, "", false, false},
			{"missing-api", "/api/missing", "Accept", "text/html", 404, "", false, false},
		} {
			t.Run(coding+"/"+tc.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest("GET", tc.path, nil)
				request.Header.Set("Accept-Encoding", coding)
				if tc.header != "" {
					request.Header.Set(tc.header, tc.argument)
				}
				handler.ServeHTTP(recorder, request)
				header := recorder.Result().Header
				if recorder.Code != tc.status {
					t.Fatal("asset/SPA status", recorder.Code, recorder.Body.String())
				}
				if tc.status == 304 {
					if recorder.Body.Len() != 0 {
						t.Fatal("conditional response gained a body")
					}
					return
				}
				wantEncoding := ""
				if tc.compressed {
					wantEncoding = coding
				}
				if header.Get("Content-Encoding") != wantEncoding {
					t.Fatal("asset encoding policy", header)
				}
				if tc.status >= 400 {
					if failure := decodeFailure(t, recorder); failure.Code != NotFound {
						t.Fatal("missing path changed error", failure)
					}
					if strings.Contains(recorder.Body.String(), "SPA shell") {
						t.Fatal("missing asset/API became HTML")
					}
					return
				}
				if string(decodeCompressed(t, wantEncoding, recorder.Body.Bytes())) != tc.body {
					t.Fatal("asset/SPA payload changed")
				}
				vary := strings.Join(header.Values("Vary"), ",")
				if !strings.Contains(vary, "Accept-Encoding") || tc.navigation && !strings.Contains(vary, "Accept") {
					t.Fatal("representation cache variation lost", header)
				}
				if tc.status == 206 && header.Get("Content-Range") != "bytes 0-19/"+strconv.Itoa(len(css)) {
					t.Fatal("identity range metadata changed", header)
				}
			})
		}
	}
}

func TestCompressedDownloadRevalidationRetainsSelectedValidator(t *testing.T) {
	payload := strings.Repeat("conditional download\n", 1024)
	for _, coding := range []string{"identity", "gzip", "br"} {
		t.Run(coding, func(t *testing.T) {
			var closes atomic.Int32
			router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
				return downloadText(payload, &closes), nil
			})
			handler := compressionTestHandler(t, DefaultCompressionConfig(), router.ServeHTTP)
			request := httptest.NewRequest("GET", "/file", nil)
			request.Header.Set("Accept-Encoding", coding)
			first := httptest.NewRecorder()
			handler.ServeHTTP(first, request)
			tag := first.Header().Get("ETag")
			if first.Code != 200 || tag == "" || closes.Load() != 1 {
				t.Fatal("download representation failed")
			}
			request = httptest.NewRequest("GET", "/file", nil)
			request.Header.Set("Accept-Encoding", coding)
			request.Header.Set("If-None-Match", tag)
			cached := httptest.NewRecorder()
			handler.ServeHTTP(cached, request)
			if cached.Code != 304 || cached.Body.Len() != 0 || cached.Header().Get("ETag") != tag || closes.Load() != 2 {
				t.Fatal("native file revalidation lost its validator or source cleanup", cached.Header(), closes.Load())
			}
			assertCORSVary(t, cached.Header(), "accept-encoding")
		})
	}
}
