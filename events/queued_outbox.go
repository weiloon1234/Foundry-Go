package events

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

type queuedEvent struct {
	MessageID   publisher.MessageID `json:"message_id"`
	Destination outbox.Destination  `json:"destination"`
	Name        Name                `json:"name"`
	Version     Version             `json:"version"`
	Payload     json.RawMessage     `json:"payload"`
}

// QueuedOutbox delivers milestone-07 event rows through the ordinary job worker.
// One declaration routes explicit destinations to their prepared buses. Earlier
// listeners may run again after failure/crash; use Topic.OutboxID for idempotency.
// Buses and queue infrastructure remain owned by their application providers.
type QueuedOutbox struct {
	definition jobs.Definition[queuedEvent]
	producers  map[outbox.Destination]*Outbox
}

func NewQueuedOutbox(policy jobs.Policy, producers ...*Outbox) (*QueuedOutbox, error) {
	if len(producers) == 0 || len(producers) > 128 {
		return nil, fault.New(fault.Invalid, "queued events require 1 to 128 outbox destinations")
	}
	result := &QueuedOutbox{definition: jobs.Define[queuedEvent]("foundry.events.deliver", 1, policy), producers: make(map[outbox.Destination]*Outbox)}
	if err := result.definition.Validate(); err != nil {
		return nil, err
	}
	for _, producer := range producers {
		if producer == nil || producer.bus == nil {
			return nil, fault.New(fault.Invalid, "queued events require prepared event outboxes")
		}
		if _, exists := result.producers[producer.destination]; exists {
			return nil, fault.New(fault.Duplicate, "queued event destination already registered")
		}
		result.producers[producer.destination] = producer
	}
	return result, nil
}
func (q *QueuedOutbox) Declaration() (jobs.Declaration, error) {
	if q == nil || q.producers == nil {
		return jobs.Declaration{}, fault.New(fault.Invalid, "queued event delivery is not initialized")
	}
	return q.definition.Declare(func(ctx context.Context, input queuedEvent) error {
		producer := q.producers[input.Destination]
		if producer == nil {
			return fault.New(fault.Missing, "queued event destination is not registered")
		}
		if input.MessageID.IsZero() {
			return fault.New(fault.Invalid, "queued event requires a publication identity")
		}
		key := topicKey{input.Name, input.Version}
		registered, ok := producer.bus.registry[key]
		if !ok {
			return fault.New(fault.Missing, "queued event schema is not registered")
		}
		operation, entry, release, err := producer.bus.begin(ctx, key, registered.schema.typ)
		if err != nil {
			return err
		}
		defer release()
		captured, err := entry.schema.parse(string(input.Payload))
		if err != nil {
			return err
		}
		frame := &outboxFrame{key: key, typ: entry.schema.typ, id: input.MessageID.Bytes()}
		frame.active.Store(true)
		defer frame.active.Store(false)
		operation = context.WithValue(operation, outboxContextKey{}, frame)
		return dispatch(operation, producer.bus, entry, captured)
	})
}
func (q *QueuedOutbox) PublicationRoute(destination outbox.Destination, dispatcher *jobs.Dispatcher) (publisher.Route, error) {
	if q == nil || q.producers[destination] == nil {
		return publisher.Route{}, fault.New(fault.Missing, "queued event destination is not registered")
	}
	if err := dispatcher.RequireDurable(); err != nil {
		return publisher.Route{}, err
	}
	return publisher.Route{Kind: eventMessageKind, Destination: destination, Publish: func(ctx context.Context, message publisher.Message) error {
		if message.Destination() != destination {
			return fault.New(fault.Invalid, "event outbox publication has another destination")
		}
		text, err := message.PayloadJSON()
		if err != nil {
			return err
		}
		origin, err := message.Origin()
		if err != nil {
			return err
		}
		restored, err := attribution.WithContext(ctx, origin)
		if err != nil {
			return err
		}
		input := queuedEvent{MessageID: message.ID(), Destination: destination, Name: Name(message.Name()), Version: Version(message.Version()), Payload: json.RawMessage(text)}
		id := model.IDFromBytes[jobs.ExecutionOf[queuedEvent]](message.ID().Bytes())
		_, err = q.definition.Dispatch(restored, dispatcher, input, jobs.Options[queuedEvent]{ID: id})
		return err
	}}, nil
}

type outboxContextKey struct{}
type outboxFrame struct {
	key    topicKey
	typ    reflect.Type
	id     [16]byte
	active atomic.Bool
}

// OutboxID identifies this topic's live durable delivery. It remains stable over
// queue retries and publisher crashes. Process-local Dispatch has no outbox ID;
// saved contexts stop representing delivery when all listeners have returned.
func (t Topic[E]) OutboxID(ctx context.Context) (outbox.ID[E], bool) {
	if ctx == nil {
		return outbox.ID[E]{}, false
	}
	frame, _ := ctx.Value(outboxContextKey{}).(*outboxFrame)
	if frame == nil || !frame.active.Load() || frame.key != (topicKey{t.name, t.version}) || frame.typ != reflect.TypeFor[E]() {
		return outbox.ID[E]{}, false
	}
	return outbox.IDFromBytes[E](frame.id), true
}
