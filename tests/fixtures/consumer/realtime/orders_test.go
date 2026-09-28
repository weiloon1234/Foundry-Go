package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"foundry.test/consumer/realtime"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type orderService struct{ calls atomic.Int32 }

func (s *orderService) Inspect(_ context.Context, id model.ID[models.Order], reply httpdto.OrderResponse) error {
	if id != reply.ID {
		return errors.New("wrong typed room")
	}
	s.calls.Add(1)
	return nil
}

func TestWebSocketKernelUsesTypedConsumerAndNativeMiddleware(t *testing.T) {
	service := &orderService{}
	var upgrades atomic.Int32
	config := websocket.DefaultConfig()
	server := websocket.DefaultServerConfig()
	server.HTTP.Address = "127.0.0.1:0"
	server.HTTP.RequestTimeout = 20 * time.Millisecond
	server.Middleware = []foundryhttp.Middleware{foundryhttp.DefineMiddleware("consumer.upgrade", func(next http.Handler) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { upgrades.Add(1); next.ServeHTTP(w, r) }), nil
	})}
	app, err := foundry.New().Register(realtime.Module(service, config, server)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	hub, err := foundation.Resolve(app.Services(), realtime.HubKey)
	if err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(run, foundation.WebSocket) }()
	ctx, timeout := context.WithTimeout(t.Context(), 5*time.Second)
	defer timeout()
	address, err := hub.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}
	peer := client.Connect(t, "ws://"+address+"/ws", http.Header{"Origin": []string{"http://" + address}}, config.MaxFrameBytes)
	order, err := model.NewID[models.Order]()
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	room := order.String()
	if err := peer.Send(ctx, websocket.Request{Version: 1, Action: websocket.Subscribe, ID: "join", Channel: realtime.Orders.ID(), Room: &room}); err != nil {
		t.Fatal(err)
	}
	if response, err := peer.Receive(ctx); err != nil || response.Type != websocket.Subscribed {
		t.Fatal("subscription failed")
	}
	// The HTTP request deadline must not become the upgraded connection lifetime.
	time.Sleep(30 * time.Millisecond)
	reply := httpdto.OrderResponse{ID: order, BuyerID: buyer}
	payload, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.Send(ctx, websocket.Request{Version: 1, Action: websocket.Message, ID: "inspect", Channel: realtime.Orders.ID(), Room: &room, Event: "inspect", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if response, err := peer.Receive(ctx); err != nil || response.Type != websocket.Acknowledged || service.calls.Load() != 1 {
		t.Fatal("typed handler did not complete")
	}
	id, err := realtime.PublishOrder(ctx, hub, order, reply)
	if err != nil {
		t.Fatal(err)
	}
	response, err := peer.Receive(ctx)
	if err != nil || response.MessageID != id {
		t.Fatal("typed publication not received")
	}
	decoded, err := client.DecodePayload(ctx, response, httpdto.OrderResponseJSON(), config.Payload)
	if err != nil || decoded != reply {
		t.Fatal("generated payload contract changed")
	}

	if err := peer.Send(ctx, websocket.Request{Version: 1, Action: websocket.Message, ID: "relay", Channel: realtime.Orders.ID(), Room: &room, Event: "relay", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []websocket.ResponseType{websocket.Accepted, websocket.EventResponse, websocket.Acknowledged} {
		frame, err := peer.Receive(ctx)
		if err != nil || frame.Type != kind {
			t.Fatal("typed consumer relay did not preserve protocol", err)
		}
	}
	// A fresh socket requests one retained event using the same generated DTO.
	reconnect := client.Connect(t, "ws://"+address+"/ws", http.Header{"Origin": []string{"http://" + address}}, config.MaxFrameBytes)
	recent := 1
	if err := reconnect.Send(ctx, websocket.Request{Version: 1, Action: websocket.Subscribe, ID: "reconnect", Channel: realtime.Orders.ID(), Room: &room, Replay: &recent}); err != nil {
		t.Fatal(err)
	}
	if frame, err := reconnect.Receive(ctx); err != nil || frame.Type != websocket.Subscribed {
		t.Fatal("consumer reconnect failed", err)
	}
	replayed, err := reconnect.Receive(ctx)
	if err != nil || !replayed.Replayed {
		t.Fatal("consumer replay missing", err)
	}
	decoded, err = client.DecodePayload(ctx, replayed, httpdto.OrderResponseJSON(), config.Payload)
	if err != nil || decoded != reply {
		t.Fatal("replay changed DTO", err)
	}
	if upgrades.Load() != 2 {
		t.Fatal("native middleware was bypassed")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("kernel did not drain")
	}
}
