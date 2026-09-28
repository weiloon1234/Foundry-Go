package events

import (
	"context"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const eventMessageKind = "event"

// Outbox binds a stable durable destination to this application's typed event
// registry. Construct it once and inject it into domain code. It does not poll
// or dispatch persisted messages, and it never owns a second bus lifetime.
type Outbox struct {
	destination outbox.Destination
	bus         *Bus
}

// PrepareOutbox performs no I/O or schema migration. Its bus may be prepared
// during application construction; enqueue/reload require that bus to be running.
func PrepareOutbox(destination outbox.Destination, bus *Bus) (*Outbox, error) {
	if err := destination.Validate(); err != nil {
		return nil, err
	}
	if bus == nil || bus.done == nil {
		return nil, fault.New(fault.Invalid, "event outbox requires a prepared bus")
	}
	return &Outbox{destination: destination, bus: bus}, nil
}

func (o *Outbox) Destination() outbox.Destination { return o.destination }
func (*Outbox) Format(state fmt.State, _ rune)    { _, _ = state.Write([]byte("event outbox")) }

func (o *Outbox) begin(ctx context.Context, key topicKey, typ reflect.Type) (context.Context, registration, outboxstore.Address, func(), error) {
	if o == nil || o.bus == nil {
		return nil, registration{}, outboxstore.Address{}, nil, fault.New(fault.Invalid, "event outbox is not prepared")
	}
	operation, entry, release, err := o.bus.begin(ctx, key, typ)
	address := outboxstore.Address{Kind: eventMessageKind, Destination: o.destination, Name: string(key.name), Version: uint32(key.version)}
	return operation, entry, address, release, err
}

// Enqueue snapshots the concrete payload and origin and inserts their outbox row
// through the supplied business transaction. Returning an ID does not establish
// outer commit or delivery. Rollback removes the row; no listeners run here.
// Delivery belongs to the worker runtime and must tolerate at-least-once attempts.
func (t Topic[E]) Enqueue(ctx context.Context, tx *database.Tx, producer *Outbox, input E) (outbox.ID[E], error) {
	if tx == nil {
		return outbox.ID[E]{}, fault.New(fault.Invalid, "event enqueue requires a transaction")
	}
	operation, _, address, release, err := producer.begin(ctx, topicKey{t.name, t.version}, reflect.TypeFor[E]())
	if err != nil {
		return outbox.ID[E]{}, err
	}
	defer release()
	origin := attribution.FromContext(operation)
	if err := origin.Validate(); err != nil {
		return outbox.ID[E]{}, err
	}
	captured, err := t.captureOwned(operation, input)
	if err != nil {
		return outbox.ID[E]{}, err
	}
	record, err := outboxstore.Append(operation, tx, address, captured.text, origin)
	if err != nil {
		return outbox.ID[E]{}, err
	}
	return outbox.IDFromBytes[E](record.ID.Bytes()), nil
}

// Stored preserves a persisted event's immutable payload and attribution. It is
// not a delivery receipt. Payload returns a fresh concrete DTO on each call.
type Stored[E any] struct {
	id        outbox.ID[E]
	payload   value.JSON[E]
	origin    attribution.Origin
	createdAt temporal.DateTime
}

func (s Stored[E]) ID() outbox.ID[E]             { return s.id }
func (s Stored[E]) Origin() attribution.Origin   { return s.origin }
func (s Stored[E]) CreatedAt() temporal.DateTime { return s.createdAt }
func (Stored[E]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("stored event")) }

// Payload decodes an independent concrete value and isolates custom codec faults.
// The caller owns the returned DTO; decoding never mutates the stored snapshot.
func (s Stored[E]) Payload() (E, error) {
	var result E
	err := callback.Isolated("decode stored event payload", func() error {
		var err error
		result, err = s.payload.Decode()
		return err
	})
	return result, err
}

// Find reloads a payload-owned ID only within this producer and topic version.
// A record from another destination/schema is absent. The registered concrete
// JSON schema is checked again; malformed persisted data fails without dispatch.
func (t Topic[E]) Find(ctx context.Context, executor database.Executor, producer *Outbox, id outbox.ID[E]) (value.Optional[Stored[E]], error) {
	operation, entry, address, release, err := producer.begin(ctx, topicKey{t.name, t.version}, reflect.TypeFor[E]())
	if err != nil {
		return value.Optional[Stored[E]]{}, err
	}
	defer release()
	record, err := outboxstore.Find(operation, executor, address, model.IDFromBytes[outboxstore.Message](id.Bytes()))
	if err != nil {
		return value.Optional[Stored[E]]{}, err
	}
	row, present := record.Get()
	if !present {
		return value.Optional[Stored[E]]{}, nil
	}
	text, err := row.Payload.Text()
	if err != nil {
		return value.Optional[Stored[E]]{}, err
	}
	var captured payload
	err = callback.Isolated("restore stored event schema", func() error {
		var err error
		captured, err = entry.schema.parse(text)
		return err
	})
	if err != nil {
		return value.Optional[Stored[E]]{}, err
	}
	typed, ok := captured.typed.(value.JSON[E])
	if !ok {
		return value.Optional[Stored[E]]{}, fault.New(fault.Internal, "stored event has the wrong payload schema")
	}
	origin, err := row.Origin.Decode()
	if err != nil {
		return value.Optional[Stored[E]]{}, err
	}
	if err := operation.Err(); err != nil {
		return value.Optional[Stored[E]]{}, err
	}
	return value.Set(Stored[E]{id: id, payload: typed, origin: origin, createdAt: row.CreatedAt}), nil
}
