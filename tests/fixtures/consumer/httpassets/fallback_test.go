package httpassets_test

import (
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpassets"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestConsumerRootAssetsAndScopedDevelopmentFallback(t *testing.T) {
	upstream := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Upstream-Method", r.Method)
		if r.Method != "HEAD" {
			_, _ = io.WriteString(w, r.URL.RequestURI())
		}
	}))
	t.Cleanup(upstream.Close)
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := httpassets.DevelopmentProxy(httputil.NewSingleHostReverseProxy(target), httpassets.Runtime("fallback"), func(r *stdhttp.Request) bool {
		return foundryhttp.IsHTMLNavigation(r, strings.TrimPrefix(r.URL.Path, "/account/"))
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := httpassets.EmbeddedSource()
	if err != nil {
		t.Fatal(err)
	}
	builder := application.New(application.DefaultSettings(), application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	builder.HTTP(func(application.Services) ([]foundryhttp.RouteRegistration, error) {
		route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "api.submit", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/api/submit"))
		return []foundryhttp.RouteRegistration{foundryhttp.HandleRaw(route, func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ foundryhttp.NoPath) { w.WriteHeader(204) })}, nil
	})
	lost := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(404)
		if r.Method != "HEAD" {
			_, _ = io.WriteString(w, "site lost")
		}
	})
	wrappedProxy := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("X-Fallback-Method", r.Method)
		proxy.ServeHTTP(w, r)
	})
	builder, err = httpassets.DeclareFallbacks(builder, source, wrappedProxy, lost)
	if err != nil {
		t.Fatal(err)
	}
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	router, err := foundation.Resolve(app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, target        string
		status                int
		body, allow, upstream string
	}{
		{"GET", "/api/submit", 405, "method_not_allowed", "POST", ""},
		{"GET", "/api/unknown", 404, "not_found", "", ""},
		{"POST", "/api/unknown", 404, "not_found", "", ""},
		{"GET", "/site.css", 200, "navy", "", ""},
		{"GET", "/lost/", 404, "site lost", "", ""},
		{"GET", "/missing.js", 404, "not_found", "", ""},
		{"GET", "/account/orders/?page=2", 200, "/account/orders/?page=2", "", "GET"},
		{"HEAD", "/account/orders/", 200, "", "", "GET"},
		{"GET", "/account/_assets/missing.js", 404, "not_found", "", ""},
		{"OPTIONS", "/account/orders/", 404, "not_found", "", ""},
	} {
		t.Run(tc.method+tc.target, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			r.Header.Set("Accept", "text/html")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) || w.Header().Get("Allow") != tc.allow || w.Header().Get("X-Upstream-Method") != tc.upstream {
				t.Fatal(w.Code, w.Header(), w.Body.String())
			}
			if tc.upstream != "" && w.Header().Get("X-Fallback-Method") != tc.method {
				t.Fatal("fallback changed method")
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
		})
	}
	// Direct assembly uses the same declaration API and exclusion semantics.
	direct, _ := foundryhttp.NewRouter()
	direct, err = httpassets.RouterFallback(direct, lost)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	direct.ServeHTTP(w, httptest.NewRequest("GET", "/api/unknown", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "not_found") {
		t.Fatal(w.Code, w.Body.String())
	}
}
