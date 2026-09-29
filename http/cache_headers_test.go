package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCacheControlPolicyRendersAndComposes(t *testing.T) {
	clock := testkit.NewClock(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	policy := CachePolicy{
		Visibility: CachePublic, MaxAge: time.Minute, SharedMaxAge: value.Set(5 * time.Minute),
		StaleWhileRevalidate: 30 * time.Second, StaleIfError: time.Hour, ExpiresFrom: clock,
	}
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/override" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if r.URL.Path == "/error" {
			_ = WriteError(w, r, NotFound)
			return
		}
		_, _ = w.Write([]byte("cacheable body"))
	}), CacheControl(policy), ETags(DefaultETagConfig()))
	if err != nil {
		t.Fatal(err)
	}
	serve := func(method, path string, header stdhttp.Header) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, nil)
		for name, values := range header {
			request.Header[name] = values
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	public := "public, max-age=60, s-maxage=300, stale-while-revalidate=30, stale-if-error=3600"
	first := serve("GET", "/", nil)
	if first.Header().Get("Cache-Control") != public || first.Header().Get("Expires") != "Mon, 28 Sep 2026 12:01:00 GMT" {
		t.Fatalf("policy=%q expires=%q", first.Header().Get("Cache-Control"), first.Header().Get("Expires"))
	}
	// ETag revalidation keeps the declared policy on 304.
	revalidated := serve("GET", "/", stdhttp.Header{"If-None-Match": {first.Header().Get("ETag")}})
	if revalidated.Code != 304 || revalidated.Header().Get("Cache-Control") != public {
		t.Fatalf("304 policy: %d %q", revalidated.Code, revalidated.Header().Get("Cache-Control"))
	}
	credentialed := serve("GET", "/", stdhttp.Header{"Authorization": {"Bearer token"}})
	if credentialed.Header().Get("Cache-Control") != "private, max-age=60, stale-while-revalidate=30, stale-if-error=3600" {
		t.Fatalf("credentialed response stayed shareable: %q", credentialed.Header().Get("Cache-Control"))
	}
	if got := serve("GET", "/override", nil).Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("handler override lost: %q", got)
	}
	if got := serve("GET", "/error", nil).Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("error response cacheable: %q", got)
	}
	if got := serve("POST", "/", nil).Header().Get("Cache-Control"); got != "" {
		t.Fatalf("unsafe method received cache policy: %q", got)
	}
	outer, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) }),
		DefineMiddleware("fixture.private", func(next stdhttp.Handler) (stdhttp.Handler, error) {
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
			}), nil
		}), CacheControl(policy))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	outer.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache policy replaced an outer no-store decision")
	}
	if got, _ := (CachePolicy{Visibility: CachePrivate, NoCache: true}).compile(); got.value != "private, max-age=0, no-cache" {
		t.Fatalf("revalidating policy=%q", got.value)
	}
	if got, _ := NoStoreCachePolicy().compile(); got.value != "no-store" {
		t.Fatal("no-store policy")
	}
	for _, invalid := range []CachePolicy{
		{}, {NoStore: true, MaxAge: time.Second}, {Visibility: CachePrivate, SharedMaxAge: value.Set(time.Second)},
		{Visibility: CachePublic, MaxAge: -time.Second}, {Visibility: CachePublic, MaxAge: time.Millisecond},
		{Visibility: CachePublic, MaxAge: 2 * maxCacheLifetime}, {Visibility: CachePublic, Immutable: true},
		{Visibility: CacheVisibility(9), MaxAge: time.Second},
	} {
		if invalid.Validate() == nil {
			t.Errorf("invalid cache policy accepted: %+v", invalid)
		}
	}
}
