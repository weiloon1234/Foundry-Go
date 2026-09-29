package http

import (
	"context"
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
	}{
		{"untrusted-peer", "192.0.2.1:3000", stdhttp.Header{"X-Forwarded-For": {"garbage"}}, "192.0.2.1"},
		{"absent-header", "10.0.0.2:3000", nil, "10.0.0.2"},
		{"first-untrusted-hop", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"1.1.1.1, 198.51.100.9, 10.0.0.1"}}, "198.51.100.9"},
		{"all-hops-trusted", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"10.0.0.3,10.0.0.1"}}, "10.0.0.3"},
		{"multiple-lines", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"203.0.113.9", "10.0.0.1"}}, "203.0.113.9"},
		{"ipv6-peer-and-client", "[2001:db8:10::2]:3000", stdhttp.Header{"X-Forwarded-For": {"2001:db8:20::9,2001:db8:10::1"}}, "2001:db8:20::9"},
		{"mapped-ipv4-peer-and-client", "[::ffff:10.0.0.2]:3000", stdhttp.Header{"X-Forwarded-For": {"::ffff:203.0.113.9"}}, "203.0.113.9"},
		{"priority-single-header", "10.0.0.2:3000", stdhttp.Header{"Cf-Connecting-Ip": {"198.51.100.8"}, "X-Forwarded-For": {"203.0.113.9"}}, "198.51.100.8"},
		{"invalid-priority-no-fallback", "10.0.0.2:3000", stdhttp.Header{"Cf-Connecting-Ip": {"invalid"}, "X-Forwarded-For": {"203.0.113.9"}}, "10.0.0.2"},
		{"repeated-single-header", "10.0.0.2:3000", stdhttp.Header{"Cf-Connecting-Ip": {"198.51.100.8", "198.51.100.9"}}, "10.0.0.2"},
		{"single-header-with-port", "10.0.0.2:3000", stdhttp.Header{"Cf-Connecting-Ip": {"198.51.100.8:4711"}}, "198.51.100.8"},
		{"client-garbage-beyond-boundary", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"unknown, not-an-ip,, 1.2.3.4"}}, "1.2.3.4"},
		{"unknown-at-boundary", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"198.51.100.9, unknown, 10.0.0.1"}}, "10.0.0.1"},
		{"empty-xff", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {""}}, "10.0.0.2"},
		{"azure-ip-port", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"203.0.113.9:51234, 10.0.0.1:443"}}, "203.0.113.9"},
		{"bracketed-ipv6-port", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"[2001:db8:20::9]:443"}}, "2001:db8:20::9"},
		{"malformed-trusted-hop", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {"198.51.100.9, private-input-invalid"}}, "10.0.0.2"},
		{"oversized-client-prefix", "10.0.0.2:3000", stdhttp.Header{"X-Forwarded-For": {strings.Repeat("x", 16<<10) + ", 203.0.113.9"}}, "203.0.113.9"},
		{"forwarded-client-quote-isolated", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {`for="1.1.1.1`, "for=198.51.100.7"}}, "198.51.100.7"},
		{"forwarded-client-quote-same-line", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {`for="1.1.1.1, for=198.51.100.7`}}, "198.51.100.7"},
		{"forwarded-malformed-trusted-hop", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=198.51.100.7, for=10.0.0.1;for=10.0.0.3"}}, "10.0.0.2"},
		{"forwarded-chain", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1, for=198.51.100.7;proto=https, for=10.0.0.1"}}, "198.51.100.7"},
		{"unknown-boundary", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1,for=unknown,for=10.0.0.1"}}, "10.0.0.1"},
		{"obfuscated-boundary", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1,for=_private"}, "X-Forwarded-For": {"203.0.113.9"}}, "10.0.0.2"},
		{"missing-for-boundary", "10.0.0.2:3000", stdhttp.Header{"Forwarded": {"for=1.1.1.1,proto=https;host=example.test"}}, "10.0.0.2"},
		{"missing-peer", "", stdhttp.Header{"X-Forwarded-For": {"203.0.113.9"}}, ""},
		{"non-ip-peer", "proxy.test:3000", stdhttp.Header{"X-Forwarded-For": {"203.0.113.9"}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.peer
			request.Header = test.headers
			got := policy.clientIP(request)
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

func TestTrustedProxyMalformedHopsNeverRejectRequests(t *testing.T) {
	var resolved []netip.Addr
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		resolved = append(resolved, ClientIP(r.Context()))
		w.WriteHeader(204)
	}), TrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Headers: []ProxyHeader{XForwardedForHeader()}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"private-input-invalid", "unknown, 1.2.3.4", "", "203.0.113.9:443", "[2001:db8::9]:443"} {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = "10.0.0.2:3000"
		request.Header.Set("X-Forwarded-For", value)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 204 || strings.Contains(response.Body.String(), "private-input-invalid") {
			t.Fatalf("%q rejected: %d %s", value, response.Code, response.Body.String())
		}
	}
	want := []string{"10.0.0.2", "1.2.3.4", "10.0.0.2", "203.0.113.9", "2001:db8::9"}
	for i, address := range want {
		if resolved[i] != netip.MustParseAddr(address) {
			t.Fatalf("resolved[%d] = %v; want %s", i, resolved[i], address)
		}
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

func TestProxyDependentMiddlewareMustRunInsideTrustedProxy(t *testing.T) {
	next := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) })
	proxy := TrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Headers: []ProxyHeader{XForwardedForHeader()}})
	public := PublicURLs(PublicURLConfig{AllowedOrigins: []Origin{"https://app.example.test"}})
	for _, chain := range [][]Middleware{
		{CSRF(CSRFConfig{}), proxy},
		{public, proxy},
		{SecurityHeaders(DefaultSecurityHeadersConfig()), CredentialRequests(), proxy},
	} {
		if _, err := ApplyMiddleware(next, chain...); err == nil || !strings.Contains(err.Error(), "TrustedProxy") {
			t.Errorf("misordered chain %v assembled: %v", middlewareIDs(chain), err)
		}
	}
	for _, chain := range [][]Middleware{
		{proxy, CSRF(CSRFConfig{}), public},
		{SecurityHeaders(DefaultSecurityHeadersConfig()), proxy, CSRF(CSRFConfig{})},
		{CSRF(CSRFConfig{})},
	} {
		if _, err := ApplyMiddleware(next, chain...); err != nil {
			t.Errorf("ordered chain %v rejected: %v", middlewareIDs(chain), err)
		}
	}
	// A route-level TrustedProxy cannot sit inside a global policy that needs it.
	route := staticRoute("proxied", stdhttp.MethodGet, "/proxied").WithMiddleware(proxy).HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) })
	router, err := NewRouter(route)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMiddleware(router, CSRF(CSRFConfig{})); err == nil {
		t.Fatal("global CSRF outside route TrustedProxy assembled")
	}
	misordered := staticRoute("misordered", stdhttp.MethodGet, "/misordered").WithMiddleware(CSRF(CSRFConfig{}), proxy).HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) {})
	if _, err := NewRouter(misordered); err == nil {
		t.Fatal("route chain with CSRF before TrustedProxy registered")
	}
}
