package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
)

func TestTrustedProxyChainAndHeaderPriority(t *testing.T) {
	policy, err := compileTrustedProxy(TrustedProxyConfig{
		Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8:10::/48")},
		Headers: []ProxyHeader{ForwardedHeader(), ClientIPHeader("CF-Connecting-IP"), XForwardedForHeader()},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, peer string
		headers    stdhttp.Header
		want       string
		reject     bool
	}{
		{"untrusted-peer", "192.0.2.1:3000", stdhttp.Header{"X-Forwarded-For": {"garbage"}}, "192.0.2.1", false},
		{"absent-header", "10.0.0.2:3000", nil, "10.0.0.2", false},
		{"first-untrusted-hop", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"1.1.1.1, 198.51.100.9, 10.0.0.1"}}, "198.51.100.9", false},
		{"all-hops-trusted", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"10.0.0.3,10.0.0.1"}}, "10.0.0.3", false},
		{"multiple-lines", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"203.0.113.9", "10.0.0.1"}}, "203.0.113.9", false},
		{"ipv6-peer-and-client", "[2001:db8:10::2]:3000", stdhttp.Header{"X-Forwarded-For": {"2001:db8:20::9,2001:db8:10::1"}}, "2001:db8:20::9", false},
		{"mapped-ipv4-peer-and-client", "[::ffff:10.0.0.2]:3000", stdhttp.Header{"X-Forwarded-For": {"::ffff:203.0.113.9"}}, "203.0.113.9", false},
		{"priority-single-header", "10.0.0.2:3000", stdhttp.Header{"Cf-Connecting-Ip": {"198.51.100.8"}, "X-Forwarded-For": {"203.0.113.9"}}, "198.51.100.8", false},
		{"invalid-priority-no-fallback", "10.0.0.2:3000", stdhttp.Header{"Cf-Connecting-Ip": {"invalid"}, "X-Forwarded-For": {"203.0.113.9"}}, "", true},
		{"forwarded-chain", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1, for=198.51.100.7;proto=https, for=10.0.0.1"}}, "198.51.100.7", false},
		{"unknown-boundary", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1,for=unknown,for=10.0.0.1"}}, "10.0.0.1", false},
		{"obfuscated-boundary", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1,for=_private"}, "X-Forwarded-For": {"203.0.113.9"}}, "10.0.0.2", false},
		{"missing-for-boundary", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1,proto=https;host=example.test"}}, "10.0.0.2", false},
		{"missing-peer", "", stdhttp.Header{"X-Forwarded-For": {"203.0.113.9"}}, "", false},
		{"non-ip-peer", "proxy.test:3000", stdhttp.Header{"X-Forwarded-For": {"203.0.113.9"}}, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.peer
			request.Header = test.headers
			got, err := policy.clientIP(request)
			if (err != nil) != test.reject {
				t.Fatalf("resolution error: %v", err)
			}
			if test.reject {
				return
			}
			var want netip.Addr
			if test.want != "" {
				want = netip.MustParseAddr(test.want)
			}
			if got != want {
				t.Fatalf("client IP = %v; want %v", got, want)
			}
		})
	}
}

func TestTrustedProxyPreservesNativeRequestAndSharedAttribution(t *testing.T) {
	origin, err := (attribution.Origin{}).WithSystem("fixture.system")
	if err != nil {
		t.Fatal(err)
	}
	origin, err = origin.WithRequest(attribution.Request{ID: "request-1", IP: netip.MustParseAddr("10.0.0.2"), UserAgent: "fixture-agent"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, err = attribution.WithContext(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(ctx, "GET", "https://internal.test/resource?q=1", nil)
	request.RemoteAddr = "10.0.0.2:3000"
	request.Header.Set("X-Forwarded-For", "198.51.100.9,10.0.0.1")
	request.Header.Set("X-Forwarded-Host", "evil.test")
	request.Header.Set("X-Forwarded-Proto", "http")
	headers := request.Header.Clone()
	nativeURL := request.URL.String()
	nativeTLS := request.TLS
	original := httptest.NewRecorder()
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if w != original || r.RemoteAddr != request.RemoteAddr || r.Host != request.Host || r.URL.String() != nativeURL || r.TLS != nativeTLS || !reflect.DeepEqual(r.Header, headers) {
			t.Error("proxy replaced native transport state")
		}
		if PeerIP(r) != netip.MustParseAddr("10.0.0.2") || ClientIP(r.Context()) != netip.MustParseAddr("198.51.100.9") {
			t.Error("socket/client identity confused")
		}
		resolved := attribution.FromContext(r.Context())
		if resolved.System() != origin.System() || resolved.Request().ID != origin.Request().ID || resolved.Request().UserAgent != origin.Request().UserAgent {
			t.Error("proxy replaced unrelated attribution")
		}
		cancel()
		if r.Context().Err() != context.Canceled {
			t.Error("request cancellation detached")
		}
		w.WriteHeader(204)
	}), TrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Headers: []ProxyHeader{XForwardedForHeader()}}))
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(original, request)
	if original.Code != 204 || ClientIP(request.Context()) != origin.Request().IP || !reflect.DeepEqual(request.Header, headers) {
		t.Fatal("proxy mutated caller request/context")
	}
}

func TestTrustedProxyRejectsMalformedTrustedInputWithSharedEnvelope(t *testing.T) {
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) { t.Error("rejected request reached handler") }), TrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Headers: []ProxyHeader{XForwardedForHeader()}}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.2:3000"
	request.Header.Set("X-Forwarded-For", "private-input-invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 400 || body.Code != BadRequest || strings.Contains(response.Body.String(), "private-input-invalid") {
		t.Fatalf("unsafe denial: %s", response.Body.String())
	}
}

func TestTrustedProxyConfigurationOwnershipAndValidation(t *testing.T) {
	config := TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.9/8")}, Headers: []ProxyHeader{XForwardedForHeader()}}
	declaration := TrustedProxy(config)
	config.Proxies[0] = netip.MustParsePrefix("192.0.2.0/24")
	config.Headers[0] = ClientIPHeader("X-Other")
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if ClientIP(r.Context()) != netip.MustParseAddr("198.51.100.9") {
			t.Error("caller mutated proxy policy")
		}
		w.WriteHeader(204)
	}), declaration)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = "10.0.0.2:3000"
			request.Header.Set("X-Forwarded-For", "198.51.100.9")
			handler.ServeHTTP(httptest.NewRecorder(), request)
		})
	}
	group.Wait()
	for _, config := range []TrustedProxyConfig{
		{Proxies: []netip.Prefix{{}}, Headers: []ProxyHeader{XForwardedForHeader()}},
		{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		{Proxies: make([]netip.Prefix, maxProxyNetworks+1)}, {Headers: make([]ProxyHeader, maxProxyHeaders+1)},
		{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/8"), netip.MustParsePrefix("10.0.0.2/8")}, Headers: []ProxyHeader{XForwardedForHeader()}},
		{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("::ffff:10.0.0.0/104")}, Headers: []ProxyHeader{XForwardedForHeader()}},
		{Headers: []ProxyHeader{{}}}, {Headers: []ProxyHeader{ClientIPHeader("")}},
		{Headers: []ProxyHeader{ClientIPHeader("Forwarded")}}, {Headers: []ProxyHeader{ClientIPHeader("x-forwarded-for")}},
		{Headers: []ProxyHeader{ClientIPHeader("X-Real-IP"), ClientIPHeader("x-real-ip")}},
	} {
		if err := config.Validate(); err == nil {
			t.Errorf("accepted invalid configuration %+v", config)
		}
		if _, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {}), TrustedProxy(config)); err == nil {
			t.Errorf("assembly accepted invalid configuration %+v", config)
		}
	}
	if err := (TrustedProxyConfig{}).Validate(); err != nil {
		t.Fatal("zero proxy policy must be valid", err)
	}
	if PeerIP(nil).IsValid() || ClientIP(nil).IsValid() {
		t.Fatal("nil request/context invented an address")
	}
}
