package websocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestRawEventsAreExplicitAndBounded(t *testing.T) {
	channel := publicChannel()
	out := ws.RawOutgoing(channel, "raw-out")
	in := ws.RawIncoming(channel, "raw-in")
	f := serve(t, registry(t, ws.Register(channel, out.Registration(), in.Handle(func(ctx context.Context, message ws.MessageContext[int64, ws.Anonymous], payload json.RawMessage) error {
		_, err := ws.Broadcast(ctx, message.Publisher, channel, out, payload)
		return err
	}))), nil, ws.DefaultConfig())
	for _, event := range f.hub.Registry().Channels()[0].Events {
		if !event.Dynamic || event.Payload.Root != "" {
			t.Fatal("raw event falsely claimed a typed schema")
		}
	}
	peer := f.dial(t, nil)
	subscribe(t, peer, "join", channel.ID(), nil)
	send(t, peer, ws.Request{Action: ws.Message, ID: "raw", Channel: channel.ID(), Event: "raw-in", Payload: json.RawMessage(`[1,"two",null]`)})
	if receive(t, peer, ws.EventResponse).Event != "raw-out" {
		t.Fatal("raw relay lost event identity")
	}
	receive(t, peer, ws.Acknowledged)
	if _, err := ws.Broadcast(t.Context(), f.hub, channel, out, json.RawMessage(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("raw publication accepted duplicate keys")
	}
}

func TestPresenceRejectsModelsAndPrivateChannelsNeedBoundGuard(t *testing.T) {
	a := authentication(t)
	rooms := ws.DefineRooms(foundryhttp.IntegerPath[int64]())
	private := ws.Private[PresenceOwner]("private", rooms, a.users, func(context.Context, Account, ws.Target[int64]) error { return nil })
	if _, err := ws.New(registry(t, ws.Register(private)), nil, ws.DefaultConfig()); err == nil {
		t.Fatal("private channel accepted no authentication owner")
	}
	unsafe := ws.WithPresence(private, textContract[Account]("display"), func(_ context.Context, subject Account) (Account, error) { return subject, nil })
	if _, err := ws.NewRegistry(ws.Register(unsafe.Channel())); err == nil {
		t.Fatal("presence exported an auth model")
	}
	zero := ws.DefineOutgoing(publicChannel(), "event", contract.JSON[Echo]{})
	if _, err := ws.NewRegistry(ws.Register(publicChannel(), zero.Registration())); err == nil {
		t.Fatal("invalid payload/channel descriptor accepted")
	}
	if _, err := ws.NewRegistry(ws.Registration{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty registration accepted")
	}
}
