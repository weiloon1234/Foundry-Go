package http

import (
	"context"
	stdhttp "net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFallbackSPAContinuationRejectsUnsafePathsBeforeServing(t *testing.T) {
	for _, prefix := range []string{"/", "/account"} {
		t.Run(prefix, func(t *testing.T) {
			publicSource := &countingAssetFS{MapFS: fstest.MapFS{"public.txt": {Data: []byte("public")}}}
			spaSource := &countingAssetFS{MapFS: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><html><head></head><body>portal</body></html>")}}}
			public := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(publicSource)))
			portal := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(spaSource)))
			var mountCalls, navigationCalls, runtimeCalls int
			mount := public.Mount("public", "/").WithNavigationFallback(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				navigationCalls++
				w.WriteHeader(404)
			})).WithMiddleware(DefineMiddleware("public.calls", func(next stdhttp.Handler) (stdhttp.Handler, error) {
				return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
					mountCalls++
					next.ServeHTTP(w, r)
				}), nil
			}))
			config := DefaultSPAConfig()
			config.Prefix = prefix
			config.Runtime = DefineSPARuntime(spaRuntimeTestCodec(), func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) {
				runtimeCalls++
				return SPARuntimeTestDTO{Text: "runtime"}, nil
			}, DefaultSPARuntimeConfig())
			native, err := NewRouter()
			if err != nil {
				t.Fatal(err)
			}
			router, err := native.WithFallbacks(mount.Fallback(DefaultFallbackConfig()))
			if err != nil {
				t.Fatal(err)
			}
			router, err = router.WithSPA("portal", portal, config)
			if err != nil {
				t.Fatal(err)
			}
			base := strings.TrimSuffix(prefix, "/")
			for _, test := range []struct {
				name, path string
				redirect   bool
			}{
				{"escaped slash", "/orders%2fprivate", false},
				{"uppercase escaped slash", "/orders%2Fprivate", false},
				{"escaped backslash", "/orders%5cprivate", false},
				{"uppercase escaped backslash", "/orders%5Cprivate", false},
				{"encoded traversal", "/%2e%2e/private", false},
				{"mixed traversal", "/.%2e/private", false},
				{"encoded dot", "/%2e", false},
				{"encoded dot segment", "/orders/%2e/private", false},
				{"literal backslash", `/orders\private`, false},
				{"literal traversal", "/orders/../private", true},
			} {
				t.Run(test.name, func(t *testing.T) {
					target := base + test.path
					response := fallbackRequest(router, "GET", target)
					if test.redirect {
						expected := fallbackRequest(native, "GET", target)
						if expected.Code < 300 || expected.Code >= 400 || response.Code != expected.Code || response.Header().Get("Location") != expected.Header().Get("Location") {
							t.Fatalf("native canonical redirect changed: status %d headers %v", response.Code, response.Header())
						}
					} else if response.Code != 400 || decodeFailure(t, response).Code != BadRequest {
						t.Fatalf("unsafe path accepted: status %d body %s", response.Code, response.Body.String())
					}
					if mountCalls != 0 || navigationCalls != 0 || runtimeCalls != 0 {
						t.Fatalf("unsafe path reached a handler: mount=%d navigation=%d runtime=%d", mountCalls, navigationCalls, runtimeCalls)
					}
					if publicSource.stats != 0 || publicSource.opens != 0 || spaSource.stats != 0 || spaSource.opens != 0 {
						t.Fatalf("unsafe path reached a source: public stat/open=%d/%d SPA stat/open=%d/%d", publicSource.stats, publicSource.opens, spaSource.stats, spaSource.opens)
					}
				})
			}
			response := fallbackRequest(router, "GET", base+"/orders/")
			if response.Code != 200 || !strings.Contains(response.Body.String(), "portal") || runtimeCalls != 1 || spaSource.stats == 0 || spaSource.opens == 0 {
				t.Fatal("healthy navigation did not reach the SPA after rejection", response.Code, response.Body.String(), runtimeCalls)
			}
			if navigationCalls != 0 || prefix == "/" && mountCalls != 1 || prefix != "/" && (mountCalls != 0 || publicSource.stats != 0 || publicSource.opens != 0) {
				t.Fatal("SPA continuation used the wrong owner", mountCalls, navigationCalls, publicSource.stats, publicSource.opens)
			}
		})
	}
}

func TestScopedAssetFallbackRootIndexBehavior(t *testing.T) {
	for _, test := range []struct {
		name  string
		index AssetPath
		spa   bool
		body  string
	}{
		{"default index", "index.html", false, "public index"},
		{"custom index", "home.html", false, "custom index"},
		{"disabled index", "", false, ""},
		{"missing index", "missing.html", false, ""},
		{"SPA supplies disabled index", "", true, "portal index"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{
				"index.html": {Data: []byte("public index")},
				"home.html":  {Data: []byte("custom index")},
			}))
			config.Index = test.index
			assets := assetsForTest(t, config)
			router, err := NewRouter()
			if err != nil {
				t.Fatal(err)
			}
			router, err = router.WithFallbacks(assets.Mount("account.files", "/account").Fallback(DefaultFallbackConfig()))
			if err != nil {
				t.Fatal(err)
			}
			if test.spa {
				portal := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{"index.html": {Data: []byte("portal index")}})))
				spa := DefaultSPAConfig()
				spa.Prefix = "/account"
				router, err = router.WithSPA("account.portal", portal, spa)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, method := range []string{"GET", "HEAD"} {
				for _, path := range []string{"/account", "/account/"} {
					t.Run(method+path, func(t *testing.T) {
						response := fallbackRequest(router, method, path+"?lang=en")
						status, location := 404, ""
						if test.body != "" {
							status = 200
							if path == "/account" {
								status, location = 308, "/account/?lang=en"
							}
						}
						if response.Code != status || response.Header().Get("Location") != location {
							t.Fatalf("root selection: status %d headers %v body %s", response.Code, response.Header(), response.Body.String())
						}
						if method == "HEAD" && response.Body.Len() != 0 {
							t.Fatal("HEAD returned a body")
						}
						if method == "GET" && status == 200 && response.Body.String() != test.body {
							t.Fatal("selected the wrong index", response.Body.String())
						}
						if method == "GET" && status == 404 && decodeFailure(t, response).Code != NotFound {
							t.Fatal("missing index did not retain JSON 404")
						}
					})
				}
			}
		})
	}
}
