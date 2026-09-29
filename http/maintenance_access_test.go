package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

func TestMaintenanceAdmissionRendersStateAndHonorsExemptionsAndBypass(t *testing.T) {
	gate, err := maintenance.New(maintenance.Policy{Exempt: []maintenance.Rule{{Method: "GET", Path: "/up"}}})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := maintenance.DigestSecret("operator-bypass-secret")
	hooks, _ := maintenance.ParseRule("POST /hooks/*")
	if err := gate.Apply(maintenance.State{Down: true, RetryAfter: 30 * time.Second, Message: "Back after the upgrade", Secret: digest, Exempt: []maintenance.Rule{hooks}}); err != nil {
		t.Fatal(err)
	}
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	handled := 0
	server, err := foundryhttp.Prepare(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		handled++
		w.WriteHeader(stdhttp.StatusNoContent)
	}), config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(maintenance.WithContext(t.Context(), gate))
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	defer func() {
		stop()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server did not stop")
		}
	}()
	ready, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	address, err := server.Ready(ready)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	transport := &stdhttp.Transport{}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*stdhttp.Request, []*stdhttp.Request) error { return stdhttp.ErrUseLastResponse }}
	do := func(method, path string, cookie *stdhttp.Cookie) (*stdhttp.Response, string) {
		t.Helper()
		request, err := stdhttp.NewRequestWithContext(t.Context(), method, "http://"+address+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, string(body)
	}
	response, body := do("GET", "/orders", nil)
	var payload foundryhttp.ErrorResponse
	if response.StatusCode != 503 || response.Header.Get("Retry-After") != "30" || json.Unmarshal([]byte(body), &payload) != nil || payload.Code != foundryhttp.Unavailable || payload.Message != "Back after the upgrade" || handled != 0 {
		t.Fatal("maintenance response did not render its state", response.StatusCode, response.Header, body)
	}
	for _, test := range []struct {
		method, path string
		status       int
	}{{"GET", "/up", 204}, {"POST", "/up", 503}, {"POST", "/hooks/github", 204}, {"POST", "/hooksx", 503}, {"GET", "/wrong-bypass-secret", 503}, {"GET", "/%75p", 503}} {
		if response, _ := do(test.method, test.path, nil); response.StatusCode != test.status {
			t.Fatal("exemption decision changed", test, response.StatusCode)
		}
	}
	response, _ = do("GET", "/operator-bypass-secret", nil)
	var bypass *stdhttp.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == maintenance.BypassCookie {
			bypass = cookie
		}
	}
	if response.StatusCode != stdhttp.StatusSeeOther || response.Header.Get("Location") != "/" || bypass == nil || !bypass.HttpOnly || bypass.SameSite != stdhttp.SameSiteLaxMode {
		t.Fatal("secret exchange did not issue a bypass cookie", response.StatusCode, response.Header)
	}
	if response, _ := do("DELETE", "/orders/1", bypass); response.StatusCode != 204 {
		t.Fatal("valid bypass cookie was rejected", response.StatusCode)
	}
	forged := &stdhttp.Cookie{Name: maintenance.BypassCookie, Value: strings.Repeat("A", len(bypass.Value))}
	if response, _ := do("GET", "/orders", forged); response.StatusCode != 503 {
		t.Fatal("forged bypass cookie was admitted")
	}
	if err := gate.Apply(maintenance.State{Down: true, Allow: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}); err != nil {
		t.Fatal(err)
	}
	if response, body := do("GET", "/orders", bypass); response.StatusCode != 204 || body != "" {
		t.Fatal("allowed peer network was rejected", response.StatusCode)
	}
	if err := gate.Apply(maintenance.State{Down: true}); err != nil {
		t.Fatal(err)
	}
	if response, _ := do("GET", "/orders", bypass); response.StatusCode != 503 || response.Header.Get("Retry-After") != "" {
		t.Fatal("cleared secret retained the bypass or retry advice")
	}
	gate.Drain()
	for _, path := range []string{"/up", "/orders"} {
		if response, _ := do("GET", path, bypass); response.StatusCode != 503 {
			t.Fatal("draining admitted an exempt or bypass request", path)
		}
	}
}

// Behind a trusted load balancer, allow networks match the forwarded client,
// not the balancer's own subnet, and the bypass cookie follows the public
// scheme. Without the admission proxy the socket peer is used.
func TestMaintenanceAdmissionResolvesClientsThroughTheTrustedProxy(t *testing.T) {
	gate, err := maintenance.New(maintenance.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := maintenance.DigestSecret("operator-bypass-secret")
	// The operator allows the private network the balancer itself lives in.
	if err := gate.Apply(maintenance.State{Down: true, Secret: digest, Allow: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}); err != nil {
		t.Fatal(err)
	}
	proxy := foundryhttp.TrustedProxy(foundryhttp.TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Headers: []foundryhttp.ProxyHeader{foundryhttp.XForwardedForHeader()}, OriginHeaders: []foundryhttp.ProxyOriginHeader{foundryhttp.ProxySchemeHeader("X-Forwarded-Proto")}})
	for name, options := range map[string][]foundryhttp.ServerOption{"proxy": {foundryhttp.WithAdmissionProxy(proxy)}, "peer": nil} {
		t.Run(name, func(t *testing.T) {
			config := foundryhttp.DefaultServerConfig()
			config.Address = "127.0.0.1:0"
			server, err := foundryhttp.Prepare(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) }), config, slog.New(slog.NewTextHandler(io.Discard, nil)), options...)
			if err != nil {
				t.Fatal(err)
			}
			ctx, stop := context.WithCancel(maintenance.WithContext(t.Context(), gate))
			done := make(chan error, 1)
			go func() { done <- server.Run(ctx) }()
			defer func() { stop(); <-done }()
			ready, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			address, err := server.Ready(ready)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			transport := &stdhttp.Transport{}
			defer transport.CloseIdleConnections()
			client := &stdhttp.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*stdhttp.Request, []*stdhttp.Request) error { return stdhttp.ErrUseLastResponse }}
			do := func(path, forwardedFor string) *stdhttp.Response {
				t.Helper()
				request, err := stdhttp.NewRequestWithContext(t.Context(), stdhttp.MethodGet, "http://"+address+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("X-Forwarded-For", forwardedFor)
				request.Header.Set("X-Forwarded-Proto", "https")
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
				return response
			}
			public := do("/", "203.0.113.9")
			exchange := do("/operator-bypass-secret", "203.0.113.9")
			var secure bool
			for _, cookie := range exchange.Cookies() {
				secure = secure || cookie.Name == maintenance.BypassCookie && cookie.Secure
			}
			if name == "proxy" {
				if public.StatusCode != stdhttp.StatusServiceUnavailable || do("/", "127.0.0.9").StatusCode != stdhttp.StatusNoContent {
					t.Fatal("allow networks did not match the forwarded client", public.StatusCode)
				}
				if exchange.StatusCode != stdhttp.StatusSeeOther || !secure {
					t.Fatal("bypass cookie ignored the trusted https scheme", exchange.StatusCode, secure)
				}
				return
			}
			// Without a proxy policy the balancer's address is the peer.
			if public.StatusCode != stdhttp.StatusNoContent {
				t.Fatal("peer admission changed without a proxy policy", public.StatusCode)
			}
		})
	}
	if _, err := foundryhttp.Prepare(stdhttp.NotFoundHandler(), foundryhttp.DefaultServerConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), foundryhttp.WithAdmissionProxy(foundryhttp.DefineMiddleware("other", func(next stdhttp.Handler) (stdhttp.Handler, error) { return next, nil }))); err == nil {
		t.Fatal("a non-proxy middleware was accepted as the admission proxy")
	}
}
