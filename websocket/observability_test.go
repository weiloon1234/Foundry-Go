package websocket_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	transport "github.com/coder/websocket"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestSocketMaintenanceAndTraceFollowConnectionOwnership(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := recorder.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	parent, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	channel := publicChannel()
	seen := make(chan tracing.Context, 1)
	incoming := ws.DefineIncoming(channel, "send", echoContract())
	registration := ws.Register(channel, incoming.Handle(func(ctx context.Context, _ ws.MessageContext[int64, ws.Anonymous], _ Echo) error {
		seen <- tracing.FromContext(ctx)
		return nil
	}))
	f := serveWith(t, registry(t, registration), nil, ws.DefaultConfig(), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := tracing.WithContext(observability.WithContext(r.Context(), recorder), parent)
			if err != nil {
				t.Error(err)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	peer := f.dial(t, nil)
	subscribe(t, peer, "join", channel.ID(), nil)
	if err := recorder.Gate().Set(true); err != nil {
		t.Fatal(err)
	}
	send(t, peer, ws.Request{Action: ws.Message, ID: "paused", Channel: channel.ID(), Event: "send", Payload: json.RawMessage(`{"text":"paused"}`)})
	if response := receive(t, peer, ws.ErrorResponse); response.Code != ws.Stopping {
		t.Fatal("paused socket admitted domain work", response.Code)
	}
	send(t, peer, ws.Request{Action: ws.Unsubscribe, ID: "leave", Channel: channel.ID()})
	receive(t, peer, ws.Unsubscribed)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	socket, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/ws", &transport.DialOptions{HTTPHeader: http.Header{"Origin": []string{f.server.URL}}, Subprotocols: []string{ws.Subprotocol}})
	cancel()
	if socket != nil {
		socket.CloseNow()
	}
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err == nil || response == nil || response.StatusCode != 503 {
		t.Fatal("maintenance accepted a new connection")
	}
	if err := recorder.Gate().Set(false); err != nil {
		t.Fatal(err)
	}
	subscribe(t, peer, "again", channel.ID(), nil)
	send(t, peer, ws.Request{Action: ws.Message, ID: "message", Channel: channel.ID(), Event: "send", Payload: json.RawMessage(`{"text":"ok"}`)})
	receive(t, peer, ws.Acknowledged)
	trace := <-seen
	if trace.IsZero() || trace.TraceID() != parent.TraceID() || trace.SpanID() == parent.SpanID() {
		t.Fatal("socket operation did not derive a child trace")
	}
	peer.Close()
	if err := f.hub.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot := recorder.Snapshot()
	if snapshot.Active != 0 {
		t.Fatal("socket shutdown stranded observations")
	}
	var connection observability.Entry
	for _, entry := range snapshot.Recent {
		if entry.Operation.Kind == observability.Socket {
			connection = entry
		}
	}
	if connection.ParentID != parent.SpanID() || connection.TraceID != parent.TraceID() {
		t.Fatal("connection did not retain handshake parent")
	}
	for _, entry := range snapshot.Recent {
		if entry.Operation.Kind == observability.SocketMessage && entry.ParentID != connection.SpanID {
			t.Fatal("message span escaped connection lineage")
		}
	}
}
