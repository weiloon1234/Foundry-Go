package websocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/ratelimit"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestRelayToOthersSkipsTheSender(t *testing.T) {
	channel := publicChannel()
	out := ws.DefineOutgoing(channel, "updated", echoContract())
	in := ws.DefineIncoming(channel, "send", echoContract())
	barrier := ws.DefineIncoming(channel, "barrier", echoContract())
	f := serve(t, registry(t, ws.Register(channel, out.Registration(), in.RelayToOthers(out), barrier.Handle(func(context.Context, ws.MessageContext[int64, ws.Anonymous], Echo) error { return nil }))), nil, ws.DefaultConfig())
	sender, other := f.dial(t, nil), f.dial(t, nil)
	subscribe(t, sender, "join", channel.ID(), room("1"))
	subscribe(t, other, "join", channel.ID(), room("1"))
	send(t, sender, ws.Request{Action: ws.Message, ID: "relay", Channel: channel.ID(), Room: room("1"), Event: "send", Payload: json.RawMessage(`{"text":"hi"}`)})
	receive(t, sender, ws.Acknowledged)
	if event := receive(t, other, ws.EventResponse); event.Event != "updated" {
		t.Fatal("relay did not reach other subscribers")
	}
	// A barrier acknowledgement proves no relayed event is queued for the sender.
	send(t, sender, ws.Request{Action: ws.Message, ID: "barrier", Channel: channel.ID(), Room: room("1"), Event: "barrier", Payload: json.RawMessage(`{"text":"x"}`)})
	receive(t, sender, ws.Acknowledged)
}

func TestErrorsCarryRequestIDsWhenKnown(t *testing.T) {
	channel := publicChannel()
	config := ws.DefaultConfig()
	config.MessageRate = ratelimit.Limit{Requests: 2, Window: time.Minute}
	f := serve(t, registry(t, ws.Register(channel)), nil, config)
	peer := f.dial(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	// Decoding stops at the unknown member, after the ID was read.
	if err := peer.SendText(ctx, []byte(`{"v":1,"action":"subscribe","id":"malformed","channel":"chat","extra":1}`)); err != nil {
		t.Fatal(err)
	}
	if r := receive(t, peer, ws.ErrorResponse); r.Code != ws.Malformed || r.ID != "malformed" {
		t.Fatal("malformed error lost its request ID", r)
	}
	subscribe(t, peer, "join", channel.ID(), nil)
	send(t, peer, ws.Request{Action: ws.Unsubscribe, ID: "limited", Channel: channel.ID()})
	if r := receive(t, peer, ws.ErrorResponse); r.Code != ws.RateLimited || r.ID != "limited" {
		t.Fatal("rate-limit error lost its request ID", r)
	}
}

func TestClientMetadataExportsInboundQueueAndRate(t *testing.T) {
	config := ws.DefaultConfig()
	description, err := ws.DescribeClient(registry(t, ws.Register(publicChannel())), config)
	if err != nil {
		t.Fatal(err)
	}
	limits := description.Limits
	if limits.InboundQueue != config.InboundQueue || limits.InboundQueue < limits.Subscriptions || limits.MessageRate.Requests != config.MessageRate.Requests || limits.MessageRate.WindowMilliseconds != config.MessageRate.Window.Milliseconds() {
		t.Fatal("client limits do not match the server policy", limits)
	}
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, code := range description.Protocol.Codes {
		found = found || code == ws.Unavailable
	}
	if !found {
		t.Fatal("retryable unavailable code is not exported")
	}
}

func TestProbeReportsLocalServingState(t *testing.T) {
	f := serve(t, registry(t, ws.Register(publicChannel())), nil, ws.DefaultConfig())
	if err := f.hub.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := f.hub.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.hub.Probe(t.Context()); !errors.Is(err, ws.Stopping) {
		t.Fatal("stopped hub probed healthy", err)
	}
}
