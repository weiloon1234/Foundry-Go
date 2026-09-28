// Package events provides concrete payload descriptors and ordered in-process
// dispatch. Durable enqueue is a separate transactional outbox boundary.
package events

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Name is a stable semantic event name, shared by registration and transport.
type Name string

// Version identifies an event payload schema. Zero is invalid.
type Version uint32

// ListenerID identifies one ordered listener within an event name/version.
type ListenerID string

// Handler receives an independent concrete payload and the dispatch context.
// It must respect cancellation and return before its owning dispatch can finish.
type Handler[E any] func(context.Context, E) error

// Topic preserves the concrete payload type through registration and dispatch.
// Define it once and reuse the descriptor rather than repeating names in calls.
type Topic[E any] struct {
	_       [0]*E
	name    Name
	version Version
}

func Define[E any](name Name, version Version) Topic[E] {
	return Topic[E]{name: name, version: version}
}
func (t Topic[E]) Name() Name       { return t.name }
func (t Topic[E]) Version() Version { return t.version }
func (t Topic[E]) Validate() error {
	return validateTopic(topicKey{t.name, t.version}, reflect.TypeFor[E]())
}

// Listener pairs a semantic name with a concrete handler. A declaration takes
// an owned copy; later slice edits cannot alter a prepared listener registry.
type Listener[E any] struct {
	name    ListenerID
	handler Handler[E]
}

func Listen[E any](name ListenerID, handler Handler[E]) Listener[E] {
	return Listener[E]{name: name, handler: handler}
}
func (Listener[E]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("event listener")) }

// Declare contributes this schema and ordered listeners. A declaration with no
// listeners is valid and permits intentional no-op dispatch. Undeclared topics
// are rejected. Payload JSON validation occurs when a concrete payload is captured.
func (t Topic[E]) Declare(listeners ...Listener[E]) (Declaration, error) {
	if err := t.Validate(); err != nil {
		return Declaration{}, err
	}
	schema := schema{key: topicKey{t.name, t.version}, typ: reflect.TypeFor[E](), parse: func(text string) (payload, error) {
		snapshot, err := value.ParseJSON[E](text)
		if err != nil {
			return payload{}, err
		}
		return storedPayload(snapshot)
	}}
	declarations := make([]listener, 0, len(listeners))
	names := make(map[ListenerID]struct{}, len(listeners))
	for _, item := range listeners {
		if err := validateListener(item.name, item.handler != nil); err != nil {
			return Declaration{}, err
		}
		if _, ok := names[item.name]; ok {
			return Declaration{}, fault.New(fault.Duplicate, "event listener is already declared")
		}
		names[item.name] = struct{}{}
		declarations = append(declarations, listener{name: item.name, invoke: func(ctx context.Context, captured payload) error {
			snapshot, ok := captured.typed.(value.JSON[E])
			if !ok {
				return fault.New(fault.Internal, "event payload does not match its registered schema")
			}
			decoded, err := snapshot.Decode()
			if err != nil {
				return err
			}
			return item.handler(ctx, decoded)
		}})
	}
	return Declaration{schema: schema, listeners: slices.Clip(declarations)}, nil
}

func (t Topic[E]) capture(input E) (payload, error) {
	snapshot, err := value.NewJSON(input)
	if err != nil {
		return payload{}, err
	}
	return storedPayload(snapshot)
}
func storedPayload[E any](snapshot value.JSON[E]) (payload, error) {
	text, err := snapshot.Text()
	if err != nil {
		return payload{}, err
	}
	return payload{typ: reflect.TypeFor[E](), typed: snapshot, text: text}, nil
}

type topicKey struct {
	name    Name
	version Version
}
type schema struct {
	key   topicKey
	typ   reflect.Type
	parse func(string) (payload, error)
}
type payload struct {
	typ   reflect.Type
	typed any
	text  string
}
type listener struct {
	name   ListenerID
	invoke func(context.Context, payload) error
}

// Declaration is the explicit heterogeneous registration boundary. Its schema
// and handlers are constructed through Topic[E], retaining concrete type checks.
// It contains no captured event payload and performs no I/O during construction.
type Declaration struct {
	schema    schema
	listeners []listener
}

func (Declaration) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("event declaration")) }

func validateListener(name ListenerID, present bool) error {
	if !identifier.Semantic(string(name)) || !present {
		return fault.New(fault.Invalid, "event listener requires a semantic name and handler")
	}
	return nil
}
