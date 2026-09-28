package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func signedTestHandler(t *testing.T, registrations ...RouteRegistration) http.Handler {
	t.Helper()
	router, err := NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, PublicURLs(PublicURLConfig{AllowedOrigins: []Origin{urlTestOrigin, "https://alias.example.test"}, Canonical: value.Set(urlTestOrigin)}))
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestSignedRawRoutePreservesNativeRequestAndWriter(t *testing.T) {
	signer := urlTestSigner(t, testkit.NewClock(urlTestTime))
	signed := textRoute("/files/{text}").Signed(signer)
	var called int
	for _, text := range []string{"a/b", "中文 + %", "email@example.test"} {
		location, err := signed.URL(t.Context(), urlTestOrigin, textPath{Text: text}, urlTestTime.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		var original *http.Request
		recorder := httptest.NewRecorder()
		handler := signedTestHandler(t, signed.HandleRaw(func(w http.ResponseWriter, r *http.Request, p textPath) {
			called++
			if w != recorder || r.URL != original.URL || r.URL.RawQuery != original.URL.RawQuery || r.RequestURI != original.RequestURI {
				t.Error("native transport was rewritten")
			}
			if p.Text != text {
				t.Error("path escaping changed")
			}
			info, ok := MatchedRoute(r.Context())
			if !ok || info.SignedURL == nil || info.SignedURL.SignatureParameter != "signature" {
				t.Error("signed route metadata absent")
			}
			if _, ok := w.(http.Flusher); !ok {
				t.Error("native writer capability lost")
			}
			w.WriteHeader(204)
		}))
		original = httptest.NewRequest("GET", location, nil)
		handler.ServeHTTP(recorder, original)
		if recorder.Code != 204 {
			t.Fatalf("signed route failed: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	if called != 3 {
		t.Fatal("handler call count incorrect")
	}
}

func TestSignedRouteRejectsBeforePathAndHandler(t *testing.T) {
	signer := urlTestSigner(t, testkit.NewClock(urlTestTime))
	var decoded, handled atomic.Int32
	codec := cookieCallbacks{format: func(s string) (string, error) { return s, nil }, parse: func(s string) (string, error) { decoded.Add(1); return s, nil }}
	route := DefineRoute(RouteSpec{ID: "files.signed", Method: GET, Access: Public}, DefinePath("/files/{text}", Param("text", codec, func(p *textPath) *string { return &p.Text }))).Signed(signer)
	location, err := route.URL(t.Context(), urlTestOrigin, textPath{Text: "original"}, urlTestTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	registration := route.HandleRaw(func(w http.ResponseWriter, r *http.Request, p textPath) { handled.Add(1); w.WriteHeader(204) })
	handler := signedTestHandler(t, registration)
	for _, bad := range []string{strings.Replace(location, "/original?", "/tampered?", 1), location + "&extra=1", strings.Replace(location, string(urlTestOrigin), "https://alias.example.test", 1)} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", bad, nil))
		if w.Code != 403 || !strings.Contains(w.Body.String(), `"error_code":"forbidden"`) {
			t.Fatalf("invalid signature status: %d %s", w.Code, w.Body.String())
		}
	}
	if decoded.Load() != 0 || handled.Load() != 0 {
		t.Fatal("domain code ran before signature verification")
	}
	// GET descriptors accept HEAD, as native routing does; an independent HEAD
	// descriptor has a different purpose and cannot reuse a GET signature.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("HEAD", location, nil))
	if w.Code != 204 || handled.Load() != 1 {
		t.Fatal("signed GET did not support HEAD")
	}
	without, err := NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	without.ServeHTTP(w, httptest.NewRequest("GET", location, nil))
	if w.Code != 500 || handled.Load() != 1 {
		t.Fatal("missing public origin policy did not fail closed")
	}
}

func TestSignedEndpointUsesOnlyVerifiedDomainQuery(t *testing.T) {
	now := testkit.NewClock(urlTestTime)
	signer := urlTestSigner(t, now)
	type query struct{ Terms []string }
	parameters := DefineQuery(RepeatedQueryParam("q", StringQuery[string](), func(q *query) *[]string { return &q.Terms }))
	base := DefineEndpoint(textRoute("/search/{text}"), parameters, EmptyBody(), EmptyResponse(204))
	signed := base.Signed(signer)
	location, err := signed.URL(t.Context(), urlTestOrigin, textPath{Text: "a/b"}, query{Terms: []string{"one + two", "中文"}}, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var called int
	router, err := NewRouter(signed.Handle(func(ctx context.Context, input Input[textPath, query, NoBody]) (NoContent, error) {
		called++
		if input.Path.Text != "a/b" || len(input.Query.Terms) != 2 || input.Query.Terms[0] != "one + two" || input.Query.Terms[1] != "中文" {
			t.Error("typed query changed")
		}
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	info, err := signed.Description()
	if err != nil || info.Route.SignedURL == nil || len(info.Query) != 1 {
		t.Fatal("signed endpoint description missing")
	}
	info.Route.SignedURL.Version = "mutated"
	if router.Routes()[0].SignedURL.Version != "v1" || router.Endpoints()[0].Route.SignedURL.Version != "v1" {
		t.Fatal("metadata is not owned")
	}
	routes := router.Routes()
	routes[0].SignedURL.Algorithm = "changed"
	if router.Routes()[0].SignedURL.Algorithm != "HMAC-SHA256" {
		t.Fatal("router exposed shared metadata")
	}
	handler, err := ApplyMiddleware(router, PublicURLs(PublicURLConfig{AllowedOrigins: []Origin{urlTestOrigin}}))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", location, nil))
	if w.Code != 204 || called != 1 {
		t.Fatalf("typed query rejected signing parameters: %d %s", w.Code, w.Body.String())
	}
	now.Advance(time.Minute)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", location, nil))
	if w.Code != 403 || called != 1 {
		t.Fatal("expired link executed domain handler")
	}
	for _, name := range []string{"expires", "signature"} {
		type reserved struct{ Value string }
		q := DefineQuery(QueryParam(name, StringQuery[string](), func(q *reserved) *string { return &q.Value }))
		invalid := DefineEndpoint(textRoute("/search/{text}"), q, EmptyBody(), EmptyResponse(204)).Signed(signer)
		if invalid.Validate() == nil {
			t.Fatal("reserved query declaration accepted")
		}
		if _, err := NewRouter(invalid.Handle(func(context.Context, Input[textPath, reserved, NoBody]) (NoContent, error) { return NoContent{}, nil })); err == nil {
			t.Fatal("invalid signed endpoint registered")
		}
	}
}

func TestSignedURLUsesTrustedPublicOriginBehindProxy(t *testing.T) {
	signed := textRoute("/files/{text}").Signed(urlTestSigner(t, testkit.NewClock(urlTestTime)))
	location, err := signed.URL(t.Context(), urlTestOrigin, textPath{Text: "document"}, urlTestTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(signed.HandleRaw(func(w http.ResponseWriter, r *http.Request, p textPath) { w.WriteHeader(204) }))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router,
		TrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, OriginHeaders: []ProxyOriginHeader{XForwardedOriginHeaders()}}),
		PublicURLs(PublicURLConfig{AllowedOrigins: []Origin{urlTestOrigin}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	relative := strings.TrimPrefix(location, string(urlTestOrigin))
	for _, trusted := range []bool{true, false} {
		r := httptest.NewRequest("GET", "http://internal.test"+relative, nil)
		r.RemoteAddr = "192.0.2.1:1234"
		if !trusted {
			r.RemoteAddr = "198.51.100.1:1234"
		}
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "app.example.test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 204
		if !trusted {
			want = 400
		}
		if w.Code != want {
			t.Fatalf("proxy trust=%v status=%d", trusted, w.Code)
		}
	}
}
