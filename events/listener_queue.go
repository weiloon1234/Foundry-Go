package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/eventseam"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// queuedListener is the job payload for one queued listener delivery.
type queuedListener struct {
	Name     Name            `json:"name"`
	Version  Version         `json:"version"`
	Listener ListenerID      `json:"listener"`
	Payload  json.RawMessage `json:"payload"`
}

// ListenerDefinition is the job schema queued listeners use. Register it (via
// ListenerQueue.Declaration) with the dispatcher the queue binds to.
func ListenerDefinition(policy jobs.Policy) jobs.Definition[queuedListener] {
	return jobs.Define[queuedListener]("foundry.events.listener", 1, policy)
}

// ListenerQueue runs one bus's queued listeners through the ordinary job worker.
// Create it once per bus, register Declaration in the job registry, and Bind
// the dispatcher before dispatching events with queued listeners. A queued
// listener receives a freshly decoded payload with the job's attribution and
// retries under the job policy; earlier deliveries are not undone on failure.
type ListenerQueue struct {
	bus        *Bus
	definition jobs.Definition[queuedListener]
	dispatcher atomic.Pointer[jobs.Dispatcher]
}

func NewListenerQueue(bus *Bus, policy jobs.Policy) (*ListenerQueue, error) {
	if bus == nil || bus.done == nil {
		return nil, fault.New(fault.Invalid, "listener queue requires a prepared bus")
	}
	queue := &ListenerQueue{bus: bus, definition: ListenerDefinition(policy)}
	if err := queue.definition.Validate(); err != nil {
		return nil, err
	}
	if !bus.listeners.CompareAndSwap(nil, queue) {
		return nil, fault.New(fault.Duplicate, "bus already has a listener queue")
	}
	return queue, nil
}
func (*ListenerQueue) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("event listener queue"))
}

// Bind selects the dispatcher queued listeners are enqueued through. It can be
// set once, after the dispatcher's registry includes Declaration.
func (q *ListenerQueue) Bind(dispatcher *jobs.Dispatcher) error {
	if q == nil || dispatcher == nil {
		return fault.New(fault.Invalid, "listener queue binding requires a dispatcher")
	}
	if !q.dispatcher.CompareAndSwap(nil, dispatcher) {
		return fault.New(fault.Duplicate, "listener queue is already bound")
	}
	return nil
}

// Declaration is the job handler that delivers one queued listener.
func (q *ListenerQueue) Declaration() (jobs.Declaration, error) {
	if q == nil || q.bus == nil {
		return jobs.Declaration{}, fault.New(fault.Invalid, "listener queue is not initialized")
	}
	return q.definition.Declare(func(ctx context.Context, input queuedListener) error {
		key := topicKey{input.Name, input.Version}
		registered, ok := q.bus.registry[key]
		if !ok {
			return fault.New(fault.Missing, "queued listener event is not registered")
		}
		operation, entry, release, err := q.bus.begin(ctx, key, registered.schema.typ)
		if err != nil {
			return err
		}
		defer release()
		for _, item := range entry.listeners {
			if item.name != input.Listener || !item.queued {
				continue
			}
			captured, err := entry.schema.parse(string(input.Payload))
			if err != nil {
				return jobs.Permanent(err)
			}
			return callback.Isolated(fmt.Sprintf("queued event listener %s", item.name), func() error { return item.invoke(operation, captured) })
		}
		return fault.New(fault.Missing, "queued listener is not registered")
	})
}

func (b *Bus) enqueueListener(ctx context.Context, key topicKey, name ListenerID, payloadJSON string) error {
	queue := b.listeners.Load()
	if queue == nil {
		return fault.New(fault.Invalid, "queued event listener requires a bound listener queue")
	}
	dispatcher := queue.dispatcher.Load()
	if dispatcher == nil {
		return fault.New(fault.Invalid, "queued event listener requires a bound listener queue")
	}
	_, err := queue.definition.Dispatch(ctx, dispatcher, queuedListener{Name: key.name, Version: key.version, Listener: name, Payload: json.RawMessage(payloadJSON)}, jobs.Options[queuedListener]{})
	return err
}

// Interceptor observes a dispatch before its listeners run; returning true
// suppresses them. testkit/events.NewFake installs one. It runs as an owned
// callback, must return promptly and must not retain the payload text.
type Interceptor func(ctx context.Context, name Name, version Version, payloadJSON string) bool

// Intercept installs interceptor on this bus until the returned restore runs.
// It is a framework test seam: token comes from the module-internal eventseam
// package, so only framework test helpers (testkit/events.NewFake) can install
// an interceptor; production code cannot silently suppress listeners.
func (b *Bus) Intercept(token eventseam.Token, interceptor Interceptor) (func(), error) {
	if !token.Valid() {
		return nil, fault.New(fault.Invalid, "event interception is reserved for framework test helpers")
	}
	if b == nil || b.done == nil || interceptor == nil {
		return nil, fault.New(fault.Invalid, "event interception requires a bus and interceptor")
	}
	if !b.interceptor.CompareAndSwap(nil, &interceptor) {
		return nil, fault.New(fault.Duplicate, "bus is already intercepted")
	}
	installed := &interceptor
	return func() { b.interceptor.CompareAndSwap(installed, nil) }, nil
}

func (b *Bus) intercepted(ctx context.Context, key topicKey, captured payload) bool {
	current := b.interceptor.Load()
	if current == nil {
		return false
	}
	suppress := false
	if callback.Isolated("event interceptor", func() error {
		suppress = (*current)(ctx, key.name, key.version, captured.text)
		return nil
	}) != nil {
		return false
	}
	return suppress
}
