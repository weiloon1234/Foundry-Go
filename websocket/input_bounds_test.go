package websocket_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	transport "github.com/coder/websocket"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func directDial(t *testing.T, f serverFixture) *transport.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	socket, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws", &transport.DialOptions{HTTPHeader: http.Header{"Origin": []string{f.server.URL}}, Subprotocols: []string{ws.Subprotocol}})
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { socket.CloseNow() })
	return socket
}
func TestBinaryAndOversizedFramesCloseTransport(t *testing.T) {
	for _, test := range []struct {
		name string
		kind transport.MessageType
		data []byte
	}{
		{"binary", transport.MessageBinary, []byte(`{}`)},
		{"oversize", transport.MessageText, []byte(strings.Repeat("x", (64<<10)+1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := serve(t, registry(t, ws.Register(publicChannel())), nil, ws.DefaultConfig())
			socket := directDial(t, f)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			_ = socket.Write(ctx, test.kind, test.data)
			if _, _, err := socket.Read(ctx); err == nil {
				t.Fatal("invalid data frame accepted")
			}
			waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 })
		})
	}
}

func TestInboundOverflowClosesSocketButKeepsActiveHandlerOwned(t *testing.T) {
	channel := publicChannel()
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var released sync.Once
	releaseNow := func() { released.Do(func() { close(release) }) }
	defer releaseNow()
	incoming := ws.DefineIncoming(channel, "block", echoContract())
	config := ws.DefaultConfig()
	config.InboundQueue = 1
	config.MaxConnections = 1
	f := serve(t, registry(t, ws.Register(channel, incoming.Handle(func(ctx context.Context, _ ws.MessageContext[int64, ws.Anonymous], _ Echo) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	}))), nil, config)
	peer := f.dial(t, nil)
	subscribe(t, peer, "join", channel.ID(), nil)
	request := ws.Request{Version: 1, Action: ws.Message, ID: "blocked", Channel: channel.ID(), Event: "block", Payload: json.RawMessage(`{"text":"wait"}`)}
	send(t, peer, request)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for i := 0; i < 5; i++ {
		if peer.Send(ctx, request) != nil {
			break
		}
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("inbound overflow did not cancel handler")
	}
	if _, err := peer.Receive(ctx); err == nil {
		t.Fatal("overflow left socket open")
	}
	if f.hub.Snapshot().Connections != 1 {
		t.Fatal("overflow abandoned active handler")
	}
	socket, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws", &transport.DialOptions{HTTPHeader: http.Header{"Origin": []string{f.server.URL}}, Subprotocols: []string{ws.Subprotocol}})
	if socket != nil {
		socket.CloseNow()
	}
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("live cancelled handler lost its connection slot")
	}
	releaseNow()
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 })
}
