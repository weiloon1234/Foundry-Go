package httpclient

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicDestinationAddressBoundaries(t *testing.T) {
	policy := PublicDestinations()
	for _, text := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "169.254.169.254", "168.63.129.16", "100.64.0.1", "0.0.0.0", "192.0.2.1", "198.18.0.1", "224.0.0.1", "240.0.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1", "2002:7f00:1::1", "2001:db8::1", "3fff::1"} {
		if policy.allowsAddress(netip.MustParseAddr(text)) {
			t.Errorf("permitted %s", text)
		}
	}
	for _, text := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !policy.allowsAddress(netip.MustParseAddr(text)) {
			t.Errorf("rejected %s", text)
		}
	}
	policy.Networks = []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")}
	if !policy.allowsAddress(netip.MustParseAddr("10.20.1.2")) || policy.allowsAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("explicit network allowlist was not exclusive")
	}
}
func TestDestinationChecksURLAndSnapshotsConfiguration(t *testing.T) {
	config := DefaultConfig("restricted")
	config.Destination = PublicDestinations()
	config.Destination.Hosts = []string{"api.example.test"}
	client, err := New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })
	config.Destination.Hosts[0] = "evil.test"
	config.Destination.Schemes[0] = HTTP
	config.Destination.Ports[0] = 80
	for _, address := range []string{"http://api.example.test/", "https://api.example.test:444/", "https://evil.test/", "https://127.0.0.1/", "https://[::ffff:127.0.0.1]/", "https://[fe80::1%25en0]/"} {
		if client.Get(address).Validate() == nil {
			t.Errorf("accepted %s", address)
		}
	}
	if err := client.Get("https://API.example.test./path").Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(config, http.DefaultTransport); err == nil {
		t.Fatal("custom transport bypassed address policy")
	}
	invalidPolicy := PublicDestinations()
	invalidPolicy.Networks = []netip.Prefix{netip.MustParsePrefix("10.0.0.1/8")}
	if invalidPolicy.Validate() == nil {
		t.Fatal("noncanonical network accepted")
	}
	invalidPolicy = DestinationPolicy{Hosts: []string{"example.test"}}
	if invalidPolicy.Validate() == nil {
		t.Fatal("silent unrestricted policy accepted")
	}
}
func TestDestinationDialPinsAndRechecksDNS(t *testing.T) {
	var lookupCalls, connections int
	dialer := newDestinationDialer(PublicDestinations(), time.Second)
	dialer.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		lookupCalls++
		if lookupCalls == 1 {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	dialer.connect = func(_ context.Context, network, address string) (net.Conn, error) {
		connections++
		if network != "tcp" || address != "8.8.8.8:443" {
			t.Errorf("not pinned: %s %s", network, address)
		}
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	connection, err := dialer.dial(t.Context(), "tcp", "example.test:443")
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if _, err := dialer.dial(t.Context(), "tcp", "example.test:443"); !errors.Is(err, DestinationDenied) {
		t.Fatal("DNS rebinding allowed", err)
	}
	dialer.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("::ffff:127.0.0.1")}, nil
	}
	if _, err := dialer.dial(t.Context(), "tcp", "example.test:443"); !errors.Is(err, DestinationDenied) {
		t.Fatal("mixed DNS answer allowed", err)
	}
	if connections != 1 {
		t.Fatal("denied addresses reached connector", connections)
	}
	dialer.lookup = func(ctx context.Context, _, _ string) ([]netip.Addr, error) { <-ctx.Done(); return nil, ctx.Err() }
	dialer.timeout = time.Millisecond
	if _, err := dialer.dial(t.Context(), "tcp", "example.test:443"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lookup timeout lost", err)
	}
}
func TestRestrictedNativeClientAndEnvironmentProxy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "http://169.254.169.254/")
			w.WriteHeader(302)
			return
		}
		io.WriteString(w, "approved")
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	number, _ := strconv.ParseUint(endpoint.Port(), 10, 16)
	config := DefaultConfig("native")
	config.Destination = PublicDestinations()
	config.Destination.Schemes = []Scheme{HTTP}
	config.Destination.Ports = []uint16{uint16(number)}
	config.Retry = NoRetries()
	denied, err := New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer denied.Close(context.Background())
	if _, err := denied.Do(t.Context(), denied.Get(server.URL)); !errors.Is(err, DestinationDenied) {
		t.Fatal("loopback was allowed", err)
	}
	if calls.Load() != 0 {
		t.Fatal("denied call reached server")
	}
	config.Destination.Networks = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	config.Destination.Hosts = []string{"127.0.0.1"}
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	allowed, err := New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer allowed.Close(context.Background())
	if allowed.owned.Proxy != nil {
		t.Fatal("restricted native transport uses environment proxy")
	}
	response, err := allowed.Do(t.Context(), allowed.Get(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	body, err := response.Text()
	if err != nil || body != "approved" {
		t.Fatal(body, err)
	}
	response, err = allowed.Do(t.Context(), allowed.Get(server.URL+"/redirect"))
	if err != nil || response.Status() != 302 {
		t.Fatal("redirect policy changed", err)
	}
	if calls.Load() != 2 {
		t.Fatal("redirect followed", calls.Load())
	}
}

func TestDeniedDestinationDoesNotOpenBodiesOrRetryDNS(t *testing.T) {
	config := DefaultConfig("denial")
	config.Destination = PublicDestinations()
	client, err := New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(context.Background())
	var opened atomic.Int32
	body := StreamBody(0, func(context.Context) (io.ReadCloser, error) { opened.Add(1); return http.NoBody, nil })
	if _, err := client.Do(t.Context(), client.Post("https://127.0.0.1/").WithBody(body)); !errors.Is(err, DestinationDenied) || opened.Load() != 0 {
		t.Fatal("denied URL opened body", opened.Load(), err)
	}
	var lookups atomic.Int32
	dialer := newDestinationDialer(config.Destination, time.Second)
	dialer.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		lookups.Add(1)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	client.owned.DialContext = dialer.dial
	if _, err := client.Do(t.Context(), client.Get("https://denied.example.test/")); !errors.Is(err, DestinationDenied) {
		t.Fatal("DNS denial lost", err)
	}
	if lookups.Load() != 1 || client.Snapshot().Attempts != 1 {
		t.Fatal("policy denial retried", lookups.Load(), client.Snapshot())
	}
}
