package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAssetNavigationFallback(t *testing.T) {
	config := DefaultAssetsConfig(FilesystemAssets(assetFixture()))
	config.CacheControl = "public, max-age=31536000, immutable"
	assets := assetsForTest(t, config)
	calls := 0
	fallback := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		calls++
		if w.Header().Get("X-Mount") != "yes" {
			t.Error("fallback ran outside mount middleware")
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(404)
		if r.Method != "HEAD" {
			_, _ = w.Write([]byte("site lost"))
		}
	})
	marked := DefineMiddleware("mount.mark", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("X-Mount", "yes")
			next.ServeHTTP(w, r)
		}), nil
	})
	mount := assets.Mount("public", "/").WithMiddleware(marked)
	router, err := NewRouter(mount.WithNavigationFallback(fallback).Register())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, accept string
		status               int
		fallback             bool
	}{
		{"GET", "/zh-cn/about/verification", "text/html;q=0.5", 404, true},
		{"HEAD", "/missing", "text/html", 404, true},
		{"GET", "/site.css", "text/html", 200, false},
		{"GET", "/missing.png", "text/html", 404, false},
		{"GET", "/missing", "application/json", 404, false},
		{"GET", "/missing", "text/html;q=0", 404, false},
		{"GET", "/nested/.hidden/page", "text/html", 404, false},
		{"POST", "/missing", "text/html", 405, false},
	} {
		t.Run(tc.method+tc.path+tc.accept, func(t *testing.T) {
			before := calls
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("Accept", tc.accept)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status || (calls == before+1) != tc.fallback {
				t.Fatalf("status=%d fallback calls=%d", w.Code, calls-before)
			}
			if tc.fallback {
				for _, header := range []string{"Cache-Control", "ETag", "Last-Modified", "Expires"} {
					if w.Header().Get(header) != "" {
						t.Fatalf("mount added %s", header)
					}
				}
				if tc.method == "HEAD" && w.Body.Len() != 0 || tc.method == "GET" && w.Body.String() != "site lost" {
					t.Fatal("fallback response changed", w.Body.String())
				}
			} else if tc.status == 200 {
				if w.Body.String() != "abcdefghij" || w.Header().Get("Cache-Control") != string(config.CacheControl) {
					t.Fatal("existing asset response changed", w.Header(), w.Body.String())
				}
			} else if !strings.Contains(w.Body.String(), `"error_code":`) || w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("lost typed routing error", w.Body.String())
			}
		})
	}
	for _, unchanged := range []AssetMount{mount, mount.WithNavigationFallback(fallback).WithNavigationFallback(nil)} {
		router, err := NewRouter(unchanged.Register())
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/missing", nil)
		r.Header.Set("Accept", "text/html")
		router.ServeHTTP(w, r)
		if w.Code != 404 || w.Header().Get("Content-Type") != "application/json" {
			t.Fatal("fallback mutated original mount or nil did not disable it")
		}
	}
}

func TestAssetNavigationFallbackRetainsSPAAndRoutePrecedence(t *testing.T) {
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{"robots.txt": {Data: []byte("robots")}})))
	spaAssets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{"index.html": {Data: []byte("shell")}})))
	fallback := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		t.Error("covering SPA or route reached mount fallback")
	})
	for _, prefix := range []string{"/", "/app"} {
		t.Run(prefix, func(t *testing.T) {
			base, err := NewRouter(assets.Mount("public", "/").WithNavigationFallback(fallback).Register(), HandleRaw(DefineRoute(RouteSpec{ID: "present", Method: GET, Access: Public}, StaticPath("/app/present")), func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) }))
			if err != nil {
				t.Fatal(err)
			}
			config := DefaultSPAConfig()
			config.Prefix, config.Exclude = prefix, []string{"/api", "/app/api"}
			router, err := base.WithSPA("spa", spaAssets, config)
			if err != nil {
				t.Fatal(err)
			}
			cases := []struct {
				path   string
				status int
			}{{"/app/missing", 200}, {"/app/present", 204}, {"/app/api/missing", 404}, {"/app/missing.png", 404}}
			if prefix == "/" {
				cases = append(cases, struct {
					path   string
					status int
				}{"/api/missing", 404})
			}
			for _, tc := range cases {
				r := httptest.NewRequest("GET", tc.path, nil)
				r.Header.Set("Accept", "text/html")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != tc.status || tc.status == 200 && w.Body.String() != "shell" {
					t.Fatal(tc.path, w.Code, w.Body.String())
				}
				if tc.status == 404 && !strings.Contains(w.Body.String(), `"error_code":"not_found"`) {
					t.Fatal("SPA exclusion or missing asset lost typed 404", w.Body.String())
				}
			}
		})
	}
}

func TestIsHTMLNavigationMethods(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "OPTIONS", "DELETE"} {
		r := httptest.NewRequest(method, "/view", nil)
		r.Header.Set("Accept", "text/html")
		if IsHTMLNavigation(r, "view") != (method == "GET" || method == "HEAD") {
			t.Fatal(method)
		}
	}
	if IsHTMLNavigation(nil, "view") {
		t.Fatal("nil request is navigation")
	}
}
