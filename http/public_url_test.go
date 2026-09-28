package http

import (
	"context"
	"crypto/tls"
	stdhttp "net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestForwardedOriginUsesOneTrustedBoundaryElement(t *testing.T) {
	policy, err := compileTrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, OriginHeaders: []ProxyOriginHeader{ForwardedOriginHeader(), XForwardedOriginHeaders()}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, peer, forwarded, host string
		reject                      bool
	}{
		{"edge", "10.0.0.2:3000", `for=203.0.113.9;proto=https;host=app.example.test`, "app.example.test", false},
		{"untrusted prefix", "10.0.0.2:3000", `for=192.0.2.1;proto=http;host=evil.test, for=203.0.113.9;proto=https;host=app.example.test`, "app.example.test", false},
		{"two trusted hops", "10.0.0.2:3000", `for=203.0.113.9;proto=https;host=app.example.test,for=10.0.0.1;proto=http;host=internal.test`, "app.example.test", false},
		{"unknown boundary", "10.0.0.2:3000", `for=203.0.113.9;proto=http;host=evil.test,for=unknown;proto=https;host=app.example.test`, "app.example.test", false},
		{"missing for boundary", "10.0.0.2:3000", `for=203.0.113.9;proto=http;host=evil.test,proto=https;host=app.example.test`, "app.example.test", false},
		{"untrusted peer ignores malformed", "203.0.113.9:3000", `invalid data`, "", false},
		{"missing host does not borrow earlier", "10.0.0.2:3000", `for=192.0.2.1;proto=https;host=evil.test,for=203.0.113.9;proto=https`, "", true},
		{"bad scheme", "10.0.0.2:3000", `for=203.0.113.9;proto=javascript;host=app.example.test`, "", true},
		{"bad authority", "10.0.0.2:3000", `for=203.0.113.9;proto=https;host="user@app.example.test"`, "", true},
		{"duplicate proto", "10.0.0.2:3000", `for=203.0.113.9;proto=https;proto=http;host=app.example.test`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://internal.test/", nil)
			r.RemoteAddr = test.peer
			r.Header.Set("Forwarded", test.forwarded)
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-Host", "fallback.test")
			origin, err := policy.forwardedOrigin(r)
			if (err != nil) != test.reject || origin.host != test.host {
				t.Fatalf("origin=%+v err=%v", origin, err)
			}
		})
	}
}

func TestOverwrittenOriginHeadersRequireUnambiguousPair(t *testing.T) {
	policy, err := compileTrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, OriginHeaders: []ProxyOriginHeader{XForwardedOriginHeaders()}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		scheme, host []string
		reject       bool
	}{
		{[]string{"https"}, []string{"app.example.test:443"}, false},
		{[]string{" HTTPS "}, []string{" APP.example.test "}, false},
		{nil, []string{"app.example.test"}, true},
		{[]string{"https"}, nil, true},
		{[]string{"https,http"}, []string{"app.example.test"}, true},
		{[]string{"https"}, []string{"app.example.test,evil.test"}, true},
		{[]string{"https", "http"}, []string{"app.example.test"}, true},
		{[]string{"https"}, []string{"app.example.test/path"}, true},
		{[]string{"https"}, []string{"app.example.test\r\nX-Bad: true"}, true},
	} {
		r := httptest.NewRequest("GET", "http://internal.test/", nil)
		r.RemoteAddr = "10.0.0.1:3000"
		r.Header["X-Forwarded-Proto"] = test.scheme
		r.Header["X-Forwarded-Host"] = test.host
		_, err := policy.forwardedOrigin(r)
		if (err != nil) != test.reject {
			t.Fatalf("scheme=%v host=%v err=%v", test.scheme, test.host, err)
		}
	}
	policy, err = compileTrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, OriginHeaders: []ProxyOriginHeader{ProxySchemeHeader("X-Forwarded-Proto")}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://app.example.test/", nil)
	r.RemoteAddr = "10.0.0.1:3000"
	r.Header.Set("X-Forwarded-Proto", "https")
	origin, err := policy.forwardedOrigin(r)
	if err != nil || origin.host != "app.example.test" || origin.scheme != "https" {
		t.Fatalf("native host not preserved: %+v %v", origin, err)
	}
}

func TestPublicURLPolicyAndHSTSUseVerifiedRequestScheme(t *testing.T) {
	proxy := TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, OriginHeaders: []ProxyOriginHeader{XForwardedOriginHeaders()}}
	public := PublicURLConfig{AllowedOrigins: []Origin{"https://app.example.test", "http://alias.example.test"}, Canonical: value.Set(Origin("https://app.example.test"))}
	security := DefaultSecurityHeadersConfig()
	security.HSTS = value.Set(HSTSPolicy{MaxAge: time.Hour})
	for _, test := range []struct {
		name, host, scheme    string
		nativeTLS, wantSecure bool
	}{
		{"TLS terminated at edge", "app.example.test", "https", false, true},
		{"public HTTP over internal TLS", "alias.example.test", "http", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := httptest.NewRequestWithContext(ctx, "GET", "http://internal.test/users/a%2Fb?name=a+b", nil)
			r.RemoteAddr = "10.0.0.1:3000"
			r.Header.Set("X-Forwarded-Proto", test.scheme)
			r.Header.Set("X-Forwarded-Host", test.host)
			if test.nativeTLS {
				r.TLS = &tls.ConnectionState{}
			}
			originalURL := *r.URL
			originalHeaders := r.Header.Clone()
			writer := httptest.NewRecorder()
			handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
				if w != writer || request.Host != r.Host || request.TLS != r.TLS || !reflect.DeepEqual(*request.URL, originalURL) || !reflect.DeepEqual(request.Header, originalHeaders) {
					t.Error("native transport mutated")
				}
				if IsSecure(request) != test.wantSecure {
					t.Error("public scheme confused with canonical base/internal TLS")
				}
				origin, ok := PublicOrigin(request.Context())
				if !ok || origin != "https://app.example.test" {
					t.Error("canonical origin missing")
				}
				absolute, err := PublicURL(request.Context(), request.URL.RequestURI())
				if err != nil || absolute != "https://app.example.test/users/a%2Fb?name=a+b" {
					t.Errorf("absolute URL %q %v", absolute, err)
				}
				cancel()
				if request.Context().Err() != context.Canceled {
					t.Error("parent cancellation lost")
				}
				w.WriteHeader(204)
			}), TrustedProxy(proxy), PublicURLs(public), SecurityHeaders(security))
			if err != nil {
				t.Fatal(err)
			}
			handler.ServeHTTP(writer, r)
			want := ""
			if test.wantSecure {
				want = "max-age=3600"
			}
			if writer.Code != 204 || writer.Header().Get("Strict-Transport-Security") != want {
				t.Fatalf("response: %d %v", writer.Code, writer.Header())
			}
			if _, ok := PublicOrigin(r.Context()); ok {
				t.Error("public URL metadata escaped into original request")
			}
		})
	}
}

func TestPublicURLsRejectHostSpoofingAndSnapshotConfig(t *testing.T) {
	config := PublicURLConfig{AllowedOrigins: []Origin{"http://app.example.test"}}
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if IsSecure(r) {
			t.Error("spoofed header made plain request secure")
		}
		w.WriteHeader(204)
	}), PublicURLs(config))
	if err != nil {
		t.Fatal(err)
	}
	config.AllowedOrigins[0] = "http://evil.test"
	for _, host := range []string{"app.example.test", "evil.test", "app.example.test@evil.test"} {
		r := httptest.NewRequest("GET", "http://app.example.test/", nil)
		r.Host = host
		r.Header.Set("Forwarded", "proto=https;host=app.example.test")
		r.Header.Set("X-Forwarded-Proto", "https")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		want := 400
		if host == "app.example.test" {
			want = 204
		}
		if response.Code != want {
			t.Errorf("host %q status=%d", host, response.Code)
		}
	}
	if IsSecure(nil) {
		t.Fatal("nil request is secure")
	}
	if _, err := PublicURL(t.Context(), "/users"); err == nil {
		t.Fatal("unbound public origin inferred")
	}
}

func TestPublicOriginConfigurationAndAbsoluteURLValidation(t *testing.T) {
	for _, config := range []PublicURLConfig{
		{}, {AllowedOrigins: []Origin{NullOrigin}}, {AllowedOrigins: []Origin{"https://example.test/path"}},
		{AllowedOrigins: []Origin{"https://EXAMPLE.test:443", "https://example.test"}},
		{AllowedOrigins: []Origin{"https://example.test"}, Canonical: value.Set(Origin("https://other.test"))},
	} {
		if config.Validate() == nil {
			t.Errorf("invalid config accepted: %+v", config)
		}
	}
	for _, headers := range [][]ProxyOriginHeader{
		{{}}, {ForwardedOriginHeader(), ForwardedOriginHeader()}, {ProxyOriginHeaders("X-Forwarded-Proto", "x-forwarded-proto")}, {ProxySchemeHeader("Forwarded")}, {ProxySchemeHeader("Host")},
	} {
		if (TrustedProxyConfig{OriginHeaders: headers}).Validate() == nil {
			t.Error("invalid origin descriptor accepted")
		}
	}
	for _, relative := range []string{"https://evil.test", "//evil.test", "/\\evil.test", "/path#fragment", "/bad%zz", "/?value=%zz", "/path\r\nLocation: https://evil.test", "/a b", ""} {
		if result, err := AbsoluteURL("https://app.example.test", relative); err == nil || result != "" {
			t.Errorf("accepted invalid relative URL %q", relative)
		}
	}
	for _, origin := range []Origin{NullOrigin, "https://user@app.example.test", "https://app.example.test/"} {
		if _, err := AbsoluteURL(origin, "/"); err == nil {
			t.Error("invalid origin accepted")
		}
	}
	got, err := AbsoluteURL("https://APP.example.test:443", "/users/a%2Fb?name=a%20b")
	if err != nil || got != "https://app.example.test/users/a%2Fb?name=a%20b" {
		t.Fatalf("normalized origin/escaping: %q %v", got, err)
	}
}

func FuzzPublicURLRemainsOnDeclaredOrigin(f *testing.F) {
	for _, path := range []string{"/users?q=a+b", "//evil.test", "/%2F%2Fevil.test", "/x%0d%0aLocation:evil", "/a%2Fb"} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		result, err := AbsoluteURL("https://app.example.test", path)
		if err != nil {
			return
		}
		if !strings.HasPrefix(result, "https://app.example.test/") || strings.ContainsAny(result, "\r\n\\#") {
			t.Fatalf("origin/response injection %q", result)
		}
	})
}
