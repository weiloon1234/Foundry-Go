package notifications

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/model"
)

// DeliveryContext gives renderers stable metadata. Renderers run once before
// durable preparation (possibly concurrently before one snapshot wins). They
// must be pure: external side effects belong to the transport, after claiming.
type DeliveryContext struct {
	Notification NotificationID
	Delivery     DeliveryID
	Name         Name
	Version      Version
	Channel      ChannelID
}
type Renderer[M, P, D any] func(context.Context, M, DeliveryContext, P) (D, error)

type Channel[M, P any] struct {
	definition channelDefinition
	render     func(context.Context, M, DeliveryContext, P) ([]byte, error)
}
type channelDefinition struct {
	id        ChannelID
	kind      string
	recipient *declarationToken
	validate  func() error
	deliver   func(context.Context, model.Identity, DeliveryID, []byte) (Outcome, error)
	client    func() (contract.Schema, error)
	realtime  *ClientRealtimeInfo
}

func (c Channel[M, P]) Validate() error {
	if !semantic(string(c.definition.id)) || c.render == nil || c.definition.validate == nil || c.definition.deliver == nil {
		return invalid()
	}
	return c.definition.validate()
}
func (c Channel[M, P]) ID() ChannelID { return c.definition.id }

// Transport is the focused custom-channel boundary. It receives a freshly
// decoded rendered DTO and stable delivery identity. An error is ambiguous;
// explicit Retry with nil error promises the service definitely did not accept.
type Transport[D any] interface {
	Deliver(context.Context, DeliveryID, D) (Outcome, error)
}

func Custom[M, P, D any](id ChannelID, payload contract.JSON[D], render Renderer[M, P, D], transport Transport[D]) Channel[M, P] {
	c := Channel[M, P]{definition: channelDefinition{id: id, kind: "custom"}}
	c.definition.validate = func() error {
		if render == nil || nilInterface(transport) {
			return invalid()
		}
		return payload.Validate()
	}
	c.render = func(ctx context.Context, subject M, delivery DeliveryContext, input P) ([]byte, error) {
		output, err := render(ctx, subject, delivery, input)
		if err != nil {
			return nil, err
		}
		return payload.Encode(ctx, output, payloadLimits())
	}
	c.definition.deliver = func(ctx context.Context, _ model.Identity, id DeliveryID, data []byte) (Outcome, error) {
		output, err := payload.Decode(ctx, data, payloadLimits())
		if err != nil {
			return Reject, nil
		}
		return transport.Deliver(ctx, id, output)
	}
	return c
}

func nilInterface(input any) bool {
	if input == nil {
		return true
	}
	v := reflect.ValueOf(input)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// DatabaseChannel retains the concrete inbox representation for typed decoding.
type DatabaseChannel[M, P, D any] struct {
	channel Channel[M, P]
	payload contract.JSON[D]
}

func Database[M, P, D any](id ChannelID, payload contract.JSON[D], render Renderer[M, P, D]) DatabaseChannel[M, P, D] {
	c := Channel[M, P]{definition: channelDefinition{id: id, kind: "database"}}
	c.definition.client = payload.Description
	c.definition.validate = func() error {
		if render == nil {
			return invalid()
		}
		return payload.Validate()
	}
	c.render = func(ctx context.Context, subject M, delivery DeliveryContext, input P) ([]byte, error) {
		output, err := render(ctx, subject, delivery, input)
		if err != nil {
			return nil, err
		}
		return payload.Encode(ctx, output, payloadLimits())
	}
	c.definition.deliver = func(ctx context.Context, _ model.Identity, _ DeliveryID, data []byte) (Outcome, error) {
		if _, err := payload.Decode(ctx, data, payloadLimits()); err != nil {
			return Reject, nil
		}
		return Accepted, nil // manager atomically creates the inbox and completes the channel
	}
	return DatabaseChannel[M, P, D]{channel: c, payload: payload}
}
func (c DatabaseChannel[M, P, D]) Channel() Channel[M, P] { return c.channel }
func (c DatabaseChannel[M, P, D]) Decode(ctx context.Context, definition Definition[P], record Record[M]) (D, error) {
	if definition.Validate() != nil || record.Name != definition.name || record.Version != definition.version {
		return *new(D), invalid()
	}
	return c.payload.Decode(ctx, []byte(record.data), payloadLimits())
}

// Email renders from the current recipient on the first eligible attempt. The
// complete message and pinned attachment references are persisted before send.
func Email[M, P any](id ChannelID, mailer *email.Mailer, render Renderer[M, P, email.Message]) Channel[M, P] {
	c := Channel[M, P]{definition: channelDefinition{id: id, kind: "email"}}
	c.definition.validate = func() error {
		if mailer == nil || render == nil {
			return invalid()
		}
		return nil
	}
	c.render = func(ctx context.Context, subject M, delivery DeliveryContext, input P) ([]byte, error) {
		message, err := render(ctx, subject, delivery, input)
		if err != nil {
			return nil, err
		}
		snapshot, err := email.CaptureMessage(message)
		if err != nil {
			return nil, err
		}
		data, err := snapshot.MarshalJSON()
		if err != nil || len(data) > MaxPayloadBytes {
			return nil, invalid()
		}
		return data, nil
	}
	c.definition.deliver = func(ctx context.Context, _ model.Identity, id DeliveryID, data []byte) (Outcome, error) {
		var snapshot email.Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return Reject, nil
		}
		message, err := snapshot.Message()
		if err != nil {
			return Reject, nil
		}
		result, err := mailer.Send(ctx, message, email.SendOptions{IdempotencyKey: email.IdempotencyKey("notification/" + id.String())})
		if result.Accepted {
			return Accepted, nil
		}
		switch email.Classification(err) {
		case email.Transient:
			return Retry, nil
		case email.Construction, email.Permanent:
			return Reject, nil
		default:
			return Unknown, nil
		}
	}
	return c
}
