package http

import (
	"crypto/tls"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestSecurityHeaderDefaultsPreserveNativeWriterAndRouting(t *testing.T) {
	original := httptest.NewRecorder()
	router, err := NewRouter(staticRoute("read", GET, "/item").HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) {
		if w != original {
			t.Error("security headers replaced native writer")
		}
		if _, ok := w.(stdhttp.Flusher); !ok {
			t.Error("native streaming interface lost")
		}
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, SecurityHeaders(DefaultSecurityHeadersConfig()))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/item", "/missing"} {
		response := original
		if path == "/missing" {
			response = httptest.NewRecorder()
		}
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		for name, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "strict-origin-when-cross-origin", "X-XSS-Protection": "0"} {
			if got := response.Header().Get(name); got != want {
				t.Errorf("%s = %q; want %q", name, got, want)
			}
		}
		if response.Header().Get("Strict-Transport-Security") != "" {
			t.Error("HSTS was implicitly enabled")
		}
		if path == "/item" && response.Code != 204 || path == "/missing" && response.Code != 404 {
			t.Errorf("routing changed: %s %d", path, response.Code)
		}
	}
}

func TestHSTSRequiresExplicitPolicyAndNativeTLS(t *testing.T) {
	for _, test := range []struct {
		policy HSTSPolicy
		want   string
	}{
		{HSTSPolicy{}, "max-age=0"},
		{HSTSPolicy{MaxAge: time.Hour}, "max-age=3600"},
		{HSTSPolicy{MaxAge: 365 * 24 * time.Hour, IncludeSubDomains: true, Preload: true}, "max-age=31536000; includeSubDomains; preload"},
	} {
		config := DefaultSecurityHeadersConfig()
		config.HSTS = value.Set(test.policy)
		handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) }), SecurityHeaders(config))
		if err != nil {
			t.Fatal(err)
		}
		for _, secure := range []bool{false, true} {
			request := httptest.NewRequest("GET", "http://example.test/", nil)
			request.Header.Set("X-Forwarded-Proto", "https")
			request.Header.Set("Forwarded", "for=192.0.2.1;proto=https")
			if secure {
				request.TLS = &tls.ConnectionState{HandshakeComplete: true}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			want := ""
			if secure {
				want = test.want
			}
			if got := response.Header().Get("Strict-Transport-Security"); got != want {
				t.Fatalf("TLS=%v HSTS=%q; want %q", secure, got, want)
			}
		}
	}
}

func TestSecurityHeaderConfigurationSnapshotAndExplicitOverride(t *testing.T) {
	config := DefaultSecurityHeadersConfig()
	config.Extra = []ResponseHeader{{"X-Version", "v1"}}
	declaration := SecurityHeaders(config)
	config.Frame = FrameSameOrigin
	config.Extra[0].Value = "changed"
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("X-Version") != "v1" {
			t.Error("caller mutated assembled defaults")
		}
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.WriteHeader(204)
	}), declaration)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
			if response.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
				t.Error("deliberate native override was lost")
			}
		})
	}
	group.Wait()
}

func TestSecurityHeaderConfigurationRejectsInvalidAndAmbiguousDeclarations(t *testing.T) {
	invalid := []SecurityHeadersConfig{
		{Frame: "ALLOW-FROM https://example.test"}, {Frame: "deny"}, {Referrer: "typo"},
		{HSTS: value.Set(HSTSPolicy{MaxAge: -time.Second})}, {HSTS: value.Set(HSTSPolicy{MaxAge: time.Millisecond})},
		{HSTS: value.Set(HSTSPolicy{MaxAge: time.Hour, IncludeSubDomains: true, Preload: true})},
		{HSTS: value.Set(HSTSPolicy{MaxAge: 365 * 24 * time.Hour, Preload: true})},
		{Extra: []ResponseHeader{{"X-Bad\r\nName", "value"}}}, {Extra: []ResponseHeader{{"X-Value", "bad\r\nInjected: yes"}}},
		{Extra: []ResponseHeader{{"X-Value", "first"}, {"x-value", "second"}}},
		{Extra: []ResponseHeader{{"X-Value", HeaderValue(strings.Repeat("x", MaxHeaderValueBytes+1))}}},
		{Extra: make([]ResponseHeader, maxSecurityExtraHeaders+1)},
	}
	for _, name := range []HeaderName{"X-Frame-Options", "Referrer-Policy", "Strict-Transport-Security", "X-Content-Type-Options", "X-XSS-Protection", "Content-Length", "Transfer-Encoding", "Connection", "Trailer", "Upgrade"} {
		invalid = append(invalid, SecurityHeadersConfig{Extra: []ResponseHeader{{name, "value"}}})
	}
	var large []ResponseHeader
	for i := range 5 {
		large = append(large, ResponseHeader{HeaderName("X-Large-" + strconv.Itoa(i)), HeaderValue(strings.Repeat("x", MaxHeaderValueBytes))})
	}
	invalid = append(invalid, SecurityHeadersConfig{Extra: large})
	for i, config := range invalid {
		if err := config.Validate(); err == nil {
			t.Errorf("accepted invalid configuration %d", i)
		}
		if _, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {}), SecurityHeaders(config)); err == nil {
			t.Errorf("assembly accepted invalid configuration %d", i)
		}
	}
	if err := (SecurityHeadersConfig{}).Validate(); err != nil {
		t.Fatal("zero policy must be valid", err)
	}
	for _, policy := range []ReferrerPolicy{ReferrerNoReferrer, ReferrerNoReferrerWhenDowngrade, ReferrerSameOrigin, ReferrerOrigin, ReferrerStrictOrigin, ReferrerOriginWhenCrossOrigin, ReferrerStrictOriginWhenCrossOrigin, ReferrerUnsafeURL} {
		if err := (SecurityHeadersConfig{Referrer: policy}).Validate(); err != nil {
			t.Errorf("rejected declared policy %q: %v", policy, err)
		}
	}
}

func TestHeaderValueRejectsControlsWithoutConfusingNames(t *testing.T) {
	for _, raw := range []HeaderValue{"", "text with spaces", "quoted=\"value\"", "horizontal\ttab", "\x80"} {
		if err := raw.Validate(); err != nil {
			t.Errorf("rejected HTTP field value: %v", err)
		}
	}
	for c := byte(0); c < 0x20; c++ {
		if c == '\t' {
			continue
		}
		if err := HeaderValue(string([]byte{'a', c, 'b'})).Validate(); err == nil {
			t.Errorf("accepted control byte %d", c)
		}
	}
	if err := HeaderValue("a\x7fb").Validate(); err == nil {
		t.Error("accepted DEL")
	}
}
