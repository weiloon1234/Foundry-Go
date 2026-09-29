package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type Direction string

const (
	ClientToServer Direction = "client_to_server"
	ServerToClient Direction = "server_to_client"
)

type eventToken struct{ marker byte }
type EventRegistration[C any] struct {
	_          [0]*C
	definition *eventDefinition
}
type eventDefinition struct {
	relay       *eventDefinition
	wire        func(context.Context, []byte, contract.JSONLimits) error
	token       *eventToken
	channel     *channelToken
	id          EventID
	direction   Direction
	dynamic     bool
	accepted    bool
	description func() (contract.Schema, error)
	validate    func() error
	invoke      func(context.Context, *Hub, ConnectionID, accessResult, []byte, contract.JSONLimits, func() bool) error
}

type Incoming[C, R, S, P any] struct {
	channel     Channel[C, R, S]
	token       *eventToken
	id          EventID
	decode      func(context.Context, []byte, contract.JSONLimits) (P, error)
	description func() (contract.Schema, error)
	validate    func() error
	dynamic     bool
	authorize   func(context.Context, MessageContext[R, S], P) error
	accepted    bool
}

func DefineIncoming[C, R, S, P any](channel Channel[C, R, S], id EventID, payload contract.JSON[P]) Incoming[C, R, S, P] {
	return Incoming[C, R, S, P]{channel: channel, token: &eventToken{}, id: id, decode: payload.Decode, description: payload.Description, validate: payload.Validate}
}

// Authorize adds a payload-aware policy after channel authorization and decoding.
// Return an authorization error on denial. A nil policy invalidates the binding.
func (e Incoming[C, R, S, P]) Authorize(check func(context.Context, MessageContext[R, S], P) error) Incoming[C, R, S, P] {
	e.authorize = check
	if check == nil {
		e.validate = func() error { return fault.New(fault.Invalid, "event authorization callback is required") }
	}
	return e
}

// AcknowledgeAccepted emits an accepted frame after decoding and authorization,
// before invoking the handler. Completion still emits ack or a stable error.
func (e Incoming[C, R, S, P]) AcknowledgeAccepted() Incoming[C, R, S, P] { e.accepted = true; return e }

// Relay is an ordinary typed handler; private room and payload policies still
// run before the corresponding outgoing event is published.
func (e Incoming[C, R, S, P]) Relay(outgoing Outgoing[C, P]) EventRegistration[C] {
	return e.relay(outgoing, false)
}

// RelayToOthers is Relay without live delivery back to the sending connection.
func (e Incoming[C, R, S, P]) RelayToOthers(outgoing Outgoing[C, P]) EventRegistration[C] {
	return e.relay(outgoing, true)
}
func (e Incoming[C, R, S, P]) relay(outgoing Outgoing[C, P], others bool) EventRegistration[C] {
	original := e.validate
	e.validate = func() error {
		if outgoing.definition == nil || outgoing.definition.channel != e.channel.token {
			return fault.New(fault.Invalid, "relay requires an outgoing event on the same channel")
		}
		if original == nil {
			return fault.New(fault.Invalid, "relay input is not initialized")
		}
		return original()
	}
	registration := e.Handle(func(ctx context.Context, message MessageContext[R, S], payload P) error {
		var options []PublishOption
		if others {
			options = append(options, ExceptConnection(message.Connection))
		}
		if room, present := message.Target.Room.Get(); present {
			_, err := Publish(ctx, message.Publisher, e.channel, room, outgoing, payload, options...)
			return err
		}
		_, err := Broadcast(ctx, message.Publisher, e.channel, outgoing, payload, options...)
		return err
	})
	registration.definition.relay = outgoing.definition
	return registration
}
func (e Incoming[C, R, S, P]) Handle(handler func(context.Context, MessageContext[R, S], P) error) EventRegistration[C] {
	d := &eventDefinition{token: e.token, channel: e.channel.token, id: e.id, direction: ClientToServer, dynamic: e.dynamic, accepted: e.accepted, description: e.description}
	d.validate = func() error {
		if e.token == nil || !identifier.Semantic(string(e.id)) || handler == nil || e.decode == nil || e.validate == nil {
			return fault.New(fault.Invalid, "invalid incoming WebSocket event")
		}
		if err := e.channel.Validate(); err != nil {
			return err
		}
		return e.validate()
	}
	d.invoke = func(ctx context.Context, hub *Hub, id ConnectionID, access accessResult, data []byte, limits contract.JSONLimits, accept func() bool) error {
		payload, err := e.decode(ctx, data, limits)
		if err != nil {
			if errors.Is(err, fault.Invalid) {
				return InvalidPayload
			}
			return err
		}
		message := MessageContext[R, S]{Publisher: hub, Connection: id, Target: access.target.(Target[R]), Subject: access.subject.(S)}
		if e.authorize != nil {
			if err := e.authorize(ctx, message, payload); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.accepted && !accept() {
			return Stopping
		}
		return handler(ctx, message, payload)
	}
	return EventRegistration[C]{definition: d}
}

type Outgoing[C, P any] struct {
	_          [0]*C
	definition *eventDefinition
	encode     func(context.Context, P, contract.JSONLimits) ([]byte, error)
}

func DefineOutgoing[C, R, S, P any](channel Channel[C, R, S], id EventID, payload contract.JSON[P]) Outgoing[C, P] {
	d := &eventDefinition{token: &eventToken{}, channel: channel.token, id: id, direction: ServerToClient, description: payload.Description}
	d.wire = func(ctx context.Context, data []byte, limits contract.JSONLimits) error {
		_, err := payload.Decode(ctx, data, limits)
		return err
	}
	d.validate = func() error {
		if err := channel.Validate(); err != nil {
			return err
		}
		return payload.Validate()
	}
	return Outgoing[C, P]{definition: d, encode: payload.Encode}
}
func (e Outgoing[C, P]) Registration() EventRegistration[C] {
	return EventRegistration[C]{definition: e.definition}
}

// RawIncoming/RawOutgoing deliberately expose dynamic JSON. Metadata marks this
// escape hatch; it is excluded from claims of typed payload schemas.
func RawIncoming[C, R, S any](channel Channel[C, R, S], id EventID) Incoming[C, R, S, json.RawMessage] {
	return Incoming[C, R, S, json.RawMessage]{channel: channel, token: &eventToken{}, id: id, dynamic: true, decode: func(ctx context.Context, data []byte, limits contract.JSONLimits) (json.RawMessage, error) {
		return rawJSON(ctx, data, limits)
	}, validate: func() error { return nil }, description: func() (contract.Schema, error) { return contract.Schema{}, nil }}
}
func RawOutgoing[C, R, S any](channel Channel[C, R, S], id EventID) Outgoing[C, json.RawMessage] {
	d := &eventDefinition{token: &eventToken{}, channel: channel.token, id: id, direction: ServerToClient, dynamic: true, validate: channel.Validate, description: func() (contract.Schema, error) { return contract.Schema{}, nil }}
	d.wire = func(ctx context.Context, data []byte, limits contract.JSONLimits) error {
		_, err := rawJSON(ctx, data, limits)
		return err
	}
	return Outgoing[C, json.RawMessage]{definition: d, encode: func(ctx context.Context, data json.RawMessage, limits contract.JSONLimits) ([]byte, error) {
		return rawJSON(ctx, data, limits)
	}}
}
func rawJSON(ctx context.Context, data json.RawMessage, limits contract.JSONLimits) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	_, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes})
	if err != nil {
		return nil, err
	}
	return slices.Clone(data), ctx.Err()
}
