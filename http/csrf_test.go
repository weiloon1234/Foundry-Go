package http_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestCSRFCrossOriginEvidenceAndSafeMethods(t *testing.T) {
	for _, test := range []struct {
		name, method, origin, site string
		status                     int
	}{
		{"safe-navigation", "GET", "https://foreign.test", "cross-site", 204},
		{"preflight", "OPTIONS", "https://foreign.test", "cross-site", 204},
		{"missing-evidence", "POST", "", "", 403},
		{"fetch-same-origin", "POST", "", "same-origin", 204},
		{"origin-fallback", "POST", "https://app.test", "", 204},
		{"canonical-origin", "POST", "HTTPS://APP.TEST:443", "", 204},
		{"downgrade", "POST", "http://app.test", "", 403},
		{"foreign-origin", "POST", "https://foreign.test", "", 403},
		{"sibling-site", "POST", "https://sibling.app.test", "same-site", 403},
		{"cross-site", "DELETE", "https://foreign.test", "cross-site", 403},
		{"contradictory", "POST", "https://foreign.test", "same-origin", 403},
		{"opaque-origin", "POST", "null", "same-origin", 403},
		{"origin-path", "POST", "https://app.test/path", "same-origin", 403},
		{"invalid-fetch", "POST", "https://app.test", "garbage", 403},
		{"user-driven", "POST", "", "none", 204},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := foundryhttp.ApplyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), foundryhttp.CSRF(foundryhttp.CSRFConfig{}))
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(test.method, "https://app.test/action", nil)
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			if test.site != "" {
				r.Header.Set("Sec-Fetch-Site", test.site)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatal(w.Code, w.Body.String())
			}
			vary := strings.Join(w.Header().Values("Vary"), ",")
			if !strings.Contains(vary, "Origin") || !strings.Contains(vary, "Sec-Fetch-Site") {
				t.Fatal("missing cache variation")
			}
		})
	}
}
func TestCSRFExplicitTrustIsImmutableAndDuplicatesFail(t *testing.T) {
	config := foundryhttp.CSRFConfig{TrustedOrigins: []foundryhttp.Origin{"https://trusted.test"}}
	middleware := foundryhttp.CSRF(config)
	config.TrustedOrigins[0] = "https://evil.test"
	handler, err := foundryhttp.ApplyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), middleware)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://trusted.test", "https://evil.test"} {
		r := httptest.NewRequest("POST", "https://app.test/action", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if (w.Code == 204) != (origin == "https://trusted.test") {
			t.Fatal("trust changed", w.Code)
		}
	}
	for _, header := range []string{"Origin", "Sec-Fetch-Site"} {
		r := httptest.NewRequest("POST", "https://app.test/action", nil)
		r.Header.Set("Origin", "https://app.test")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("repeated evidence accepted")
		}
	}
	for _, origins := range [][]foundryhttp.Origin{{"null"}, {"*"}, {"https://one.test", "https://ONE.test:443"}} {
		if err := (foundryhttp.CSRFConfig{TrustedOrigins: origins}).Validate(); err == nil {
			t.Fatal("invalid trust accepted")
		}
	}
}
func TestCSRFUsesOnlyTrustedProxyOrigin(t *testing.T) {
	config := foundryhttp.TrustedProxyConfig{}
	config.Proxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	config.OriginHeaders = []foundryhttp.ProxyOriginHeader{foundryhttp.XForwardedOriginHeaders()}
	handler, err := foundryhttp.ApplyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), foundryhttp.TrustedProxy(config), foundryhttp.CSRF(foundryhttp.CSRFConfig{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []string{"127.0.0.1:9000", "192.0.2.1:9000"} {
		r := httptest.NewRequest("POST", "http://internal.test/action", nil)
		r.RemoteAddr = peer
		r.Header.Set("Origin", "https://app.test")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "app.test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if (w.Code == 204) != (peer == "127.0.0.1:9000") {
			t.Fatal("untrusted proxy origin", w.Code)
		}
	}
}
