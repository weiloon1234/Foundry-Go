package http

import (
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

type securityCapture struct{ events []SecurityRequestEvent }

func (*securityCapture) ObserveRequest(context.Context, RequestEvent) {}
func (s *securityCapture) ObserveSecurityRequest(_ context.Context, e SecurityRequestEvent) {
	s.events = append(s.events, e)
}
func TestSecurityObserverTrustsOnlyConfiguredPeerAndExcludesQuery(t *testing.T) {
	owner := newHandlerLifetime()
	policy, err := compileTrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}, Headers: []ProxyHeader{XForwardedForHeader()}})
	if err != nil {
		t.Fatal(err)
	}
	owner.proxy = &policy
	capture := &securityCapture{}
	config := DefaultServerConfig()
	config.MaxBodyBytes = 1
	handler := owner.wrap(stdhttp.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)), config, capture)

	for _, peer := range []string{"10.0.0.2:1234", "198.51.100.4:1234"} {
		request := httptest.NewRequest("POST", "/../../.env?password=private", strings.NewReader("big"))
		request.RemoteAddr = peer
		request.Header.Set("X-Forwarded-For", "203.0.113.9")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		event := capture.events[len(capture.events)-1]
		expected := netip.MustParseAddr("203.0.113.9")
		if strings.HasPrefix(peer, "198.") {
			expected = netip.MustParseAddr("198.51.100.4")
		}
		if response.Code != 413 || event.ClientIP != expected || strings.Contains(event.Path, "private") || event.Request.RequestID == "" {
			t.Fatal(event)
		}
	}
	path, truncated := boundedSecurityPath("/" + strings.Repeat("a", 600) + "\n")
	if !truncated || len(path) > MaxSecurityPathBytes || strings.Contains(path, "\n") {
		t.Fatal("unbounded path")
	}

}
