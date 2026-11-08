package http

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// A fallback answers unmatched GET/HEAD requests only; routes, method errors
// and other methods keep their ordinary responses.
func TestRouterFallbackServesUnmatchedNavigationOnly(t *testing.T) {
	t.Parallel()
	router, err := NewRouter(HandleRaw(DefineRoute(RouteSpec{ID: "home", Method: GET, Access: Public}, StaticPath("/home")), func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) {
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	root := fallbackPrefix(t, "/")
	var matched RouteID
	withFallback, err := router.WithFallbacks(DefineFallback("pages.missing", root, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		info, _ := MatchedRoute(r.Context())
		matched = info.ID
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(stdhttp.StatusNotFound)
		if r.Method != "HEAD" {
			_, _ = w.Write([]byte("<h1>Missing</h1>"))
		}
	}), DefaultFallbackConfig()))
	if err != nil {
		t.Fatal(err)
	}
	serve := func(handler stdhttp.Handler, method, target string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, target, nil))
		return response
	}
	if response := serve(withFallback, "GET", "/nowhere"); response.Code != 404 || response.Body.String() != "<h1>Missing</h1>" || matched != "pages.missing" {
		t.Fatal("fallback did not serve", response.Code, response.Body.String(), matched)
	}
	if response := serve(withFallback, "HEAD", "/nowhere"); response.Code != 404 || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
		t.Fatal("HEAD fallback", response.Code)
	}
	if response := serve(withFallback, "GET", "/home"); response.Code != 204 {
		t.Fatal("declared route intercepted", response.Code)
	}
	if response := serve(withFallback, "POST", "/home"); response.Code != 405 || decodeFailure(t, response).Code != MethodNotAllowed {
		t.Fatal("method error intercepted", response.Code)
	}
	if response := serve(withFallback, "POST", "/nowhere"); response.Code != 404 || decodeFailure(t, response).Code != NotFound {
		t.Fatal("non-navigation method reached fallback", response.Code)
	}
	if response := serve(router, "GET", "/nowhere"); response.Code != 404 || decodeFailure(t, response).Code != NotFound {
		t.Fatal("original router changed", response.Code)
	}
	var fallback RouteInfo
	for _, info := range withFallback.Routes() {
		if info.ID == "pages.missing" {
			fallback = info
		}
	}
	if !fallback.Fallback || !fallback.Raw || fallback.Method != GET || fallback.Path != "/{path...}" || len(router.Routes()) != 1 {
		t.Fatal("fallback inspection", fallback)
	}
	if _, err := withFallback.WithFallbacks(DefineFallback("pages.other", root, stdhttp.NotFoundHandler(), DefaultFallbackConfig())); !errors.Is(err, fault.Duplicate) {
		t.Fatal("second fallback accepted", err)
	}
	if _, err := router.WithFallbacks(DefineFallback("home", root, stdhttp.NotFoundHandler(), DefaultFallbackConfig())); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate fallback ID accepted", err)
	}
	if _, err := router.WithFallbacks(DefineFallback("pages.missing", root, nil, DefaultFallbackConfig())); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil fallback accepted", err)
	}
}
