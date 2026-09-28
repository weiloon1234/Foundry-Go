package websocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestTypedPublicationRoutesAndDeduplicatesSubscriptions(t *testing.T) {
	channel := publicChannel()
	out := ws.DefineOutgoing(channel, "updated", echoContract())
	in := ws.DefineIncoming(channel, "send", echoContract())
	f := serve(t, registry(t, ws.Register(channel, out.Registration(), in.Handle(func(context.Context, ws.MessageContext[int64, ws.Anonymous], Echo) error { return nil }))), nil, ws.DefaultConfig())
	a, b, c := f.dial(t, nil), f.dial(t, nil), f.dial(t, nil)
	subscribe(t, a, "a-wide", channel.ID(), nil)
	subscribe(t, a, "a-room", channel.ID(), room("1"))
	subscribe(t, b, "b", channel.ID(), room("2"))
	subscribe(t, c, "c", channel.ID(), nil)
	id, err := ws.Publish(t.Context(), f.hub, channel, int64(1), out, Echo{"private-room"})
	if err != nil {
		t.Fatal(err)
	}
	got := receive(t, a, ws.EventResponse)
	if got.MessageID != id || got.Room == nil || *got.Room != "1" {
		t.Fatal("publication identity/room lost")
	}
	barrier := func(peer *client.Client, target *string) {
		send(t, peer, ws.Request{Action: ws.Message, ID: "barrier", Channel: channel.ID(), Room: target, Event: "send", Payload: json.RawMessage(`{"text":"ok"}`)})
		receive(t, peer, ws.Acknowledged)
	}
	barrier(b, room("2"))
	barrier(c, nil)
	id, err = ws.Broadcast(t.Context(), f.hub, channel, out, Echo{"all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []*client.Client{a, b, c} {
		event := receive(t, peer, ws.EventResponse)
		if event.MessageID != id || event.Room != nil {
			t.Fatal("broadcast metadata changed")
		}
	}
	barrier(a, room("1"))
	barrier(b, room("2"))
	barrier(c, nil)
	for _, test := range []struct {
		request ws.Request
		code    ws.Code
	}{
		{ws.Request{Action: ws.Subscribe, ID: "duplicate", Channel: channel.ID(), Room: room("1")}, ws.AlreadySubscribed},
		{ws.Request{Action: ws.Message, ID: "direction", Channel: channel.ID(), Room: room("1"), Event: "updated", Payload: json.RawMessage(`{"text":"x"}`)}, ws.WrongDirection},
		{ws.Request{Action: ws.Message, ID: "unknown", Channel: channel.ID(), Room: room("1"), Event: "missing", Payload: json.RawMessage(`{}`)}, ws.UnknownEvent},
		{ws.Request{Action: ws.Message, ID: "payload", Channel: channel.ID(), Room: room("1"), Event: "send", Payload: json.RawMessage(`{"Text":"case-folded"}`)}, ws.InvalidPayload},
		{ws.Request{Action: ws.Message, ID: "membership", Channel: channel.ID(), Room: room("3"), Event: "send", Payload: json.RawMessage(`{"text":"x"}`)}, ws.NotSubscribed},
	} {
		send(t, a, test.request)
		if got := receive(t, a, ws.ErrorResponse); got.Code != test.code {
			t.Fatalf("expected %s, got %s", test.code, got.Code)
		}
	}
	send(t, a, ws.Request{Action: ws.Unsubscribe, ID: "leave", Channel: channel.ID(), Room: room("1")})
	receive(t, a, ws.Unsubscribed)
	if _, err := ws.Publish(t.Context(), f.hub, channel, int64(1), out, Echo{"after-leave"}); err != nil {
		t.Fatal(err)
	}
	barrier(a, nil)
}

func TestRegistryAndPublicationPreserveDeclarationIdentity(t *testing.T) {
	channel := publicChannel()
	other := publicChannel()
	event := ws.DefineOutgoing(other, "updated", echoContract())
	if _, err := ws.NewRegistry(ws.Register(channel, event.Registration())); !errors.Is(err, fault.Invalid) {
		t.Fatal("same-name foreign channel event registered")
	}
	if _, err := ws.NewRegistry(ws.Register(channel), ws.Register(other)); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate channel accepted")
	}
	event = ws.DefineOutgoing(channel, "updated", echoContract())
	f := serve(t, registry(t, ws.Register(channel, event.Registration())), nil, ws.DefaultConfig())
	if _, err := ws.Publish(t.Context(), f.hub, other, int64(1), event, Echo{}); !errors.Is(err, fault.Missing) {
		t.Fatal("forged channel published")
	}
	forged := ws.DefineOutgoing(channel, "updated", echoContract())
	if _, err := ws.Publish(t.Context(), f.hub, channel, int64(1), forged, Echo{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("forged event published")
	}
	metadata := f.hub.Registry().Channels()
	metadata[0].Events[0].Payload.Types[0].ID = "changed"
	if f.hub.Registry().Channels()[0].Events[0].Payload.Types[0].ID == "changed" {
		t.Fatal("metadata snapshot aliases registry")
	}
}

func TestConcurrentPublishAndUnsubscribeDisconnect(t *testing.T) {
	var leaves atomic.Int32
	channel := publicChannel().WithHooks(ws.Hooks[int64, ws.Anonymous]{Left: func(context.Context, ws.LeaveContext[int64]) error { leaves.Add(1); return nil }})
	out := ws.DefineOutgoing(channel, "updated", echoContract())
	config := ws.DefaultConfig()
	config.MaxConnections = 16
	config.OutboundQueue = 64
	f := serve(t, registry(t, ws.Register(channel, out.Registration())), nil, config)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		peer := f.dial(t, nil)
		subscribe(t, peer, "join", channel.ID(), room("1"))
		group.Go(func() {
			for j := 0; j < 5; j++ {
				_, _ = ws.Publish(t.Context(), f.hub, channel, int64(1), out, Echo{"race"})
			}
		})
		group.Go(func() {
			defer peer.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			// Closing immediately competes with dispatching this unsubscribe.
			_ = peer.Send(ctx, ws.Request{Version: ws.ProtocolVersion, Action: ws.Unsubscribe, ID: "leave", Channel: channel.ID(), Room: room("1")})
		})
	}
	group.Wait()
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 && f.hub.Snapshot().Subscriptions == 0 })
	if leaves.Load() != 8 {
		t.Fatal("unsubscribe/disconnect race duplicated or lost leave cleanup")
	}
}
