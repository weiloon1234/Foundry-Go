package notifications

import (
	"context"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// RealtimeMessage carries a stable notification ID across client reconnects;
// clients should ignore an ID they already handled.
// Publication is local admission/distributed attempt, not an end-user receipt.
type RealtimeMessage[D any] struct {
	ID      NotificationID `json:"id"`
	Name    Name           `json:"name"`
	Version Version        `json:"version"`
	Data    D              `json:"data"`
}

func (RealtimeMessage[D]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("realtime notification"))
}

// Realtime owns an ownership-enforcing channel and server-only event. Register
// Registration with the existing WebSocket registry before constructing its hub.
// No arbitrary channel can be substituted into a notification delivery binding.
type Realtime[C any, M model.Identifiable, K comparable, D any] struct {
	recipient Recipient[M, K]
	channel   websocket.Channel[C, K, M]
	event     websocket.Outgoing[C, RealtimeMessage[D]]
	eventName websocket.EventID
	payload   contract.JSON[RealtimeMessage[D]]
}

func DefineRealtime[C any, M model.Identifiable, K comparable, D any](recipient Recipient[M, K], channel websocket.ChannelID, rooms websocket.Rooms[K], event websocket.EventID, data contract.JSON[D]) Realtime[C, M, K, D] {
	payload := realtimeContract(data)
	owned := websocket.OwnedRooms[C](channel, rooms, recipient.guard, recipient.provider.Reference())
	return Realtime[C, M, K, D]{recipient: recipient, channel: owned, event: websocket.DefineOutgoing(owned, event, payload), eventName: event, payload: payload}
}
func (r Realtime[C, M, K, D]) Registration() websocket.Registration {
	return websocket.Register(r.channel, r.event.Registration())
}
func (r Realtime[C, M, K, D]) WithReplay(config websocket.ReplayConfig) Realtime[C, M, K, D] {
	r.channel = r.channel.WithReplay(config)
	return r
}

// RealtimeChannel preserves model/payload ownership while reusing the private
// room declaration. Its renderer cannot choose a different recipient room.
func RealtimeChannel[C any, M model.Identifiable, K comparable, P, D any](id ChannelID, realtime Realtime[C, M, K, D], publisher websocket.PublisherSource, render Renderer[M, P, D]) Channel[M, P] {
	c := Channel[M, P]{definition: channelDefinition{id: id, kind: "realtime", recipient: realtime.recipient.token}}
	c.definition.client = realtime.payload.Description
	c.definition.realtime = &ClientRealtimeInfo{Channel: realtime.channel.ID(), Event: realtime.eventName}
	c.definition.validate = func() error {
		if nilInterface(publisher) || render == nil || !semantic(string(realtime.eventName)) {
			return invalid()
		}
		if err := realtime.recipient.Validate(); err != nil {
			return err
		}
		if err := realtime.channel.Validate(); err != nil {
			return err
		}
		return realtime.payload.Validate()
	}
	c.render = func(ctx context.Context, subject M, delivery DeliveryContext, input P) ([]byte, error) {
		data, err := render(ctx, subject, delivery, input)
		if err != nil {
			return nil, err
		}
		return realtime.payload.Encode(ctx, RealtimeMessage[D]{delivery.Notification, delivery.Name, delivery.Version, data}, payloadLimits())
	}
	c.definition.deliver = func(ctx context.Context, identity model.Identity, _ DeliveryID, data []byte) (Outcome, error) {
		reference, err := realtime.recipient.provider.Parse(identity)
		if err != nil {
			return Reject, nil
		}
		message, err := realtime.payload.Decode(ctx, data, payloadLimits())
		if err != nil {
			return Reject, nil
		}
		_, err = websocket.Publish(ctx, publisher, realtime.channel, reference.Key(), realtime.event, message)
		if err != nil {
			// A declaration mismatch can never succeed. Only a failure that
			// provably happened before anything was published (local
			// admission, a closing hub, encoding) is retried; a distributed
			// failure may follow partial fan-out or history and stays
			// uncertain for an operator decision.
			if errorgraph.Is(err, fault.Invalid) {
				return Reject, nil
			}
			if websocket.NotPublished(err) {
				return Retry, nil
			}
			return Unknown, nil
		}
		return Accepted, nil
	}
	return c
}

func realtimeContract[D any](data contract.JSON[D]) contract.JSON[RealtimeMessage[D]] {
	schema, err := data.Description()
	if err != nil {
		return contract.JSON[RealtimeMessage[D]]{}
	}
	typ := reflect.TypeFor[RealtimeMessage[D]]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	text, number, id := root+":text", root+":version", root+":id"
	schema.Types = append(schema.Types,
		contract.Type{ID: text, Kind: contract.StringKind},
		contract.Type{ID: number, Kind: contract.IntegerKind, Bits: 32},
		contract.Type{ID: id, Kind: contract.StringKind, Format: contract.UUIDFormat},
		contract.Type{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "id", Type: id, Required: true}, {Name: "name", Type: text, Required: true},
			{Name: "version", Type: number, Required: true}, {Name: "data", Type: schema.Root, Required: true},
		}},
	)
	schema.Root = root
	return contract.DefineJSON[RealtimeMessage[D]](schema)
}
