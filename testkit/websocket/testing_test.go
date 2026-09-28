package websocket_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	wstest "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func TestConnectOwnsRealProtocolCleanupAndTypedPayloads(t *testing.T) {
	channel := websocket.Public[struct{}]("notices", websocket.DefineRooms(foundryhttp.StringPath[string]()))
	event := websocket.DefineOutgoing(channel, "updated", contract.StringJSON[string]())
	registry, err := websocket.NewRegistry(websocket.Register(channel, event.Registration()))
	if err != nil {
		t.Fatal(err)
	}
	hub, err := websocket.New(registry, nil, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := hub.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	server := httptest.New(t, hub)
	var retained *wstest.Client
	t.Run("connection", func(t *testing.T) {
		retained = wstest.Connect(t, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", http.Header{"Origin": {server.URL}}, 64<<10)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		if err := retained.Send(ctx, websocket.Request{Version: websocket.ProtocolVersion, Action: websocket.Subscribe, ID: "subscribe", Channel: "notices"}); err != nil {
			t.Fatal(err)
		}
		response, err := retained.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wstest.AssertType(t, response, websocket.Subscribed)
		if _, err := websocket.Broadcast(ctx, hub, channel, event, "typed payload"); err != nil {
			t.Fatal(err)
		}
		response, err = retained.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wstest.AssertType(t, response, websocket.EventResponse)
		limits := contract.JSONLimits{Bytes: 64 << 10, Depth: 16, Nodes: 1000, Steps: 1000, Issues: 8}
		payload, err := wstest.DecodePayload(ctx, response, contract.StringJSON[string](), limits)
		if err != nil || payload != "typed payload" {
			t.Fatal("typed protocol payload changed", err)
		}
		response.Type = websocket.ErrorResponse
		if _, err := wstest.DecodePayload(ctx, response, contract.StringJSON[string](), limits); !errors.Is(err, fault.Invalid) {
			t.Fatal("non-event decoded as payload", err)
		}
		if _, err := retained.Receive(nil); !errors.Is(err, fault.Invalid) {
			t.Fatal("nil read context accepted")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := retained.Receive(ctx); err == nil {
		t.Fatal("test cleanup left a WebSocket usable")
	}
}
