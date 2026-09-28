package http_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestCredentialRequestsProtectBeforeHandlerAndRespectProxyTrust(t *testing.T) {
	for _, test := range []struct {
		name, method, scheme, forwarded string
		trusted                         bool
		want                            int
	}{
		{name: "native TLS", method: "POST", scheme: "https", want: 204},
		{name: "plaintext", method: "POST", scheme: "http", want: 400},
		{name: "untrusted forwarding", method: "POST", scheme: "http", forwarded: "https", want: 400},
		{name: "trusted forwarding", method: "POST", scheme: "http", forwarded: "https", trusted: true, want: 204},
		{name: "GET", method: "GET", scheme: "https", want: 405},
		{name: "HEAD", method: "HEAD", scheme: "https", want: 405},
		{name: "PUT", method: "PUT", scheme: "https", want: 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			middleware := []foundryhttp.Middleware{}
			if test.trusted {
				middleware = append(middleware, foundryhttp.TrustedProxy(foundryhttp.TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, OriginHeaders: []foundryhttp.ProxyOriginHeader{foundryhttp.ProxySchemeHeader("X-Forwarded-Proto")}}))
			}
			middleware = append(middleware, foundryhttp.CredentialRequests())
			handler, err := foundryhttp.ApplyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) }), middleware...)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(test.method, test.scheme+"://app.test/reset", nil)
			request.RemoteAddr = "192.0.2.1:1234"
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-Proto", test.forwarded)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, request)
			if w.Code != test.want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("credential request policy", w.Code)
			}
			if (calls == 1) != (test.want == 204) {
				t.Fatal("rejected request reached handler")
			}
			if test.want == 405 && w.Header().Get("Allow") != "POST" {
				t.Fatal("missing supported method")
			}
		})
	}
}
