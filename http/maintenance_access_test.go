package http_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	config.MaintenanceReadPaths = []string{"/operations"}
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
	do := func(method, path, accept string, cookie *stdhttp.Cookie) (*stdhttp.Response, string) {
		t.Helper()
		request, err := stdhttp.NewRequestWithContext(t.Context(), method, "http://"+address+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if accept != "" {
			request.Header.Set("Accept", accept)
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
	response, body := do("GET", "/orders", "", nil)
	var payload foundryhttp.ErrorResponse
	if response.StatusCode != 503 || response.Header.Get("Retry-After") != "30" || json.Unmarshal([]byte(body), &payload) != nil || payload.Code != foundryhttp.Unavailable || payload.Message != "Back after the upgrade" || handled != 0 {
		t.Fatal("maintenance response did not render its state", response.StatusCode, response.Header, body)
	}
	const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	response, body = do("GET", "/", browserAccept, nil)
	_, style, ok := strings.Cut(body, "<style>")
	style, _, closed := strings.Cut(style, "</style>")
	if !ok || !closed {
		t.Fatal("maintenance page has no inline style", body)
	}
	styleHash := sha256.Sum256([]byte(style))
	policy := "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(styleHash[:]) + "'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	assertPausedHeaders := func(t *testing.T, response *stdhttp.Response, contentType string) {
		t.Helper()
		if response.StatusCode != 503 || response.Header.Get("Content-Type") != contentType || response.Header.Get("Retry-After") != "30" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("maintenance response headers changed", response.StatusCode, response.Header)
		}
		for _, line := range response.Header.Values("Vary") {
			for _, name := range strings.Split(line, ",") {
				if strings.EqualFold(strings.TrimSpace(name), "Accept") {
					return
				}
			}
		}
		t.Fatal("maintenance response does not vary on Accept", response.Header)
	}
	for _, path := range []string{"/", "/login"} {
		t.Run("browser "+path, func(t *testing.T) {
			response, body := do("GET", path, browserAccept, nil)
			assertPausedHeaders(t, response, "text/html; charset=utf-8")
			if response.Header.Get("Content-Security-Policy") != policy || !strings.Contains(body, "<h1>Down for maintenance</h1>") || !strings.Contains(body, "Back after the upgrade") {
				t.Fatal("browser maintenance page changed", response.Header, body)
			}
		})
	}
	t.Run("browser HEAD", func(t *testing.T) {
		response, body := do("HEAD", "/login", browserAccept, nil)
		assertPausedHeaders(t, response, "text/html; charset=utf-8")
		if response.Header.Get("Content-Security-Policy") != policy || body != "" {
			t.Fatal("HEAD did not preserve HTML headers without a body", response.Header, body)
		}
	})
	for _, test := range []struct {
		name, method, path, accept string
	}{
		{"JSON", "GET", "/", "application/json"},
		{"wildcard", "GET", "/", "*/*"},
		{"no Accept", "GET", "/", ""},
		{"HTML excluded", "GET", "/", "text/html;q=0"},
		{"asset", "GET", "/app.js", browserAccept},
		{"POST", "POST", "/", browserAccept},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, body := do(test.method, test.path, test.accept, nil)
			assertPausedHeaders(t, response, "application/json")
			var payload foundryhttp.ErrorResponse
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatal(err, body)
			}
			expected, err := json.Marshal(foundryhttp.ErrorResponse{Status: 503, Code: foundryhttp.Unavailable, Message: "Back after the upgrade", RequestID: payload.RequestID})
			if err != nil {
				t.Fatal(err)
			}
			if body != string(expected)+"\n" || response.Header.Get("Content-Security-Policy") != "" {
				t.Fatal("JSON maintenance envelope changed", body)
			}
		})
	}
	if handled != 0 {
		t.Fatal("a rejected maintenance request reached the handler")
	}
	for _, test := range []struct {
		method, path string
		status       int
	}{{"GET", "/up", 204}, {"POST", "/up", 503}, {"POST", "/hooks/github", 204}, {"POST", "/hooksx", 503}, {"GET", "/operations", 204}, {"HEAD", "/operations", 204}, {"POST", "/operations", 503}, {"GET", "/wrong-bypass-secret", 503}, {"GET", "/%75p", 503}} {
		if response, _ := do(test.method, test.path, browserAccept, nil); response.StatusCode != test.status {
			t.Fatal("exemption decision changed", test, response.StatusCode)
		}
	}
	response, _ = do("GET", "/operator-bypass-secret", browserAccept, nil)
	var bypass *stdhttp.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == maintenance.BypassCookie {
			bypass = cookie
		}
	}
	if response.StatusCode != stdhttp.StatusSeeOther || response.Header.Get("Location") != "/" || bypass == nil || !bypass.HttpOnly || bypass.SameSite != stdhttp.SameSiteLaxMode {
		t.Fatal("secret exchange did not issue a bypass cookie", response.StatusCode, response.Header)
	}
	if response, _ := do("DELETE", "/orders/1", browserAccept, bypass); response.StatusCode != 204 {
		t.Fatal("valid bypass cookie was rejected", response.StatusCode)
	}
	forged := &stdhttp.Cookie{Name: maintenance.BypassCookie, Value: strings.Repeat("A", len(bypass.Value))}
	if response, _ := do("GET", "/orders", browserAccept, forged); response.StatusCode != 503 {
		t.Fatal("forged bypass cookie was admitted")
	}
	if err := gate.Apply(maintenance.State{Down: true, Allow: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}); err != nil {
		t.Fatal(err)
	}
	if response, body := do("GET", "/orders", browserAccept, bypass); response.StatusCode != 204 || body != "" {
		t.Fatal("allowed peer network was rejected", response.StatusCode)
	}
	if err := gate.Apply(maintenance.State{Down: true}); err != nil {
		t.Fatal(err)
	}
	if response, body := do("GET", "/orders", browserAccept, bypass); response.StatusCode != 503 || response.Header.Get("Retry-After") != "" || !strings.Contains(body, "This site is temporarily unavailable for scheduled maintenance. Please check back soon.") {
		t.Fatal("cleared secret retained the bypass or retry advice")
	}
	if err := gate.Apply(maintenance.State{Down: true, Message: `<script>alert(1)</script> & "x"`}); err != nil {
		t.Fatal(err)
	}
	if response, body := do("GET", "/", browserAccept, nil); response.StatusCode != 503 || strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt; &amp; &#34;x&#34;") {
		t.Fatal("maintenance message was not escaped", body)
	}
	gate.Drain()
	for _, path := range []string{"/up", "/orders"} {
		if response, _ := do("GET", path, browserAccept, bypass); response.StatusCode != 503 || response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Vary") != "" {
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
