package websocket_test

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	transport "github.com/coder/websocket"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestUpgradeEnforcesOriginVersionAndQueryPolicy(t *testing.T) {
	channel := publicChannel()
	f := serve(t, registry(t, ws.Register(channel)), nil, ws.DefaultConfig())
	for _, test := range []struct {
		name, suffix string
		headers      http.Header
		protocols    []string
		status       int
	}{
		{"originless", "", nil, []string{ws.Subprotocol}, 403},
		{"cross-origin", "", http.Header{"Origin": []string{"https://attacker.invalid"}}, []string{ws.Subprotocol}, 403},
		{"null-origin", "", http.Header{"Origin": []string{"null"}}, []string{ws.Subprotocol}, 403},
		{"repeated-origin", "", http.Header{"Origin": []string{f.server.URL, f.server.URL}}, []string{ws.Subprotocol}, 403},
		{"no-protocol", "", http.Header{"Origin": []string{f.server.URL}}, nil, 400},
		{"query-credential", "?token=private", http.Header{"Origin": []string{f.server.URL}}, []string{ws.Subprotocol}, 400},
		{"empty-query", "?", http.Header{"Origin": []string{f.server.URL}}, []string{ws.Subprotocol}, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			socket, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws"+test.suffix, &transport.DialOptions{HTTPHeader: test.headers, Subprotocols: test.protocols})
			if socket != nil {
				socket.CloseNow()
			}
			if response != nil && response.Body != nil {
				defer response.Body.Close()
			}
			if err == nil || response == nil || response.StatusCode != test.status {
				t.Fatal("unsafe handshake accepted or wrong rejection status")
			}
		})
	}
	peer := f.dial(t, nil)
	for _, data := range []string{`{"v":2,"action":"subscribe","id":"v","channel":"chat"}`, `{"v":1,"action":"subscribe","id":"x","channel":"missing"}`, `{"v":1,"action":"subscribe","id":"x","channel":"chat","room":"01"}`} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		err := peer.SendText(ctx, []byte(data))
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		receive(t, peer, ws.ErrorResponse)
	}
	subscribe(t, peer, "valid", channel.ID(), room("1"))
}

func TestUpgradeOriginOptInsAreExactAndOwned(t *testing.T) {
	config := ws.DefaultConfig()
	config.AllowOriginless = true
	config.AdditionalOrigins = []foundryhttp.Origin{"https://client.example"}
	f := serve(t, registry(t, ws.Register(publicChannel())), nil, config)
	config.AdditionalOrigins[0] = "https://changed.example"
	for _, test := range []struct {
		origin  string
		allowed bool
	}{
		{"", true}, {"https://client.example", true}, {"http://client.example", false},
		{"https://client.example:444", false}, {"https://changed.example", false}, {"null", false},
	} {
		t.Run(test.origin, func(t *testing.T) {
			headers := make(http.Header)
			if test.origin != "" {
				headers.Set("Origin", test.origin)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			socket, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws", &transport.DialOptions{HTTPHeader: headers, Subprotocols: []string{ws.Subprotocol}})
			if socket != nil {
				socket.CloseNow()
			}
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if (err == nil) != test.allowed {
				t.Fatal("origin policy disagreed with explicit configuration")
			}
		})
	}
}

func TestUpgradeUsesOnlyTrustedProxyOrigin(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(map[bool]string{false: "untrusted", true: "trusted"}[trusted], func(t *testing.T) {
			config := foundryhttp.TrustedProxyConfig{}
			if trusted {
				config.Proxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
				config.OriginHeaders = []foundryhttp.ProxyOriginHeader{foundryhttp.XForwardedOriginHeaders()}
			}
			f := serveWith(t, registry(t, ws.Register(publicChannel())), nil, ws.DefaultConfig(), func(next http.Handler) http.Handler {
				handler, err := foundryhttp.ApplyMiddleware(next, foundryhttp.TrustedProxy(config))
				if err != nil {
					t.Fatal(err)
				}
				return handler
			})
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			socket, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws", &transport.DialOptions{Subprotocols: []string{ws.Subprotocol}, HTTPHeader: http.Header{"Origin": []string{"https://public.example"}, "X-Forwarded-Proto": []string{"https"}, "X-Forwarded-Host": []string{"public.example"}}})
			if socket != nil {
				socket.CloseNow()
			}
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if (err == nil) != trusted {
				t.Fatal("proxy origin trusted the wrong peer boundary")
			}
		})
	}
}
