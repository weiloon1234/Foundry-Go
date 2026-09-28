package eventqueries

import (
	"context"
	"errors"
	"sync"

	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

var Durable = foundation.NewKey[*events.Outbox]("consumer.events.durable")
var Enqueued = foundation.NewKey[*EnqueueTrace]("consumer.events.enqueue_trace")

// EnqueueTrace records returned IDs for fixture assertions and injects an error
// after enqueue. These in-memory IDs are deliberately not commit/delivery receipts.
type EnqueueTrace struct {
	mu      sync.Mutex
	ids     []outbox.ID[RecordCreated]
	failure error
}

func (r *EnqueueTrace) Record(id outbox.ID[RecordCreated]) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
	return r.failure
}
func (r *EnqueueTrace) IDs() []outbox.ID[RecordCreated] {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]outbox.ID[RecordCreated](nil), r.ids...)
}
func (r *EnqueueTrace) Reject(err error) { r.mu.Lock(); defer r.mu.Unlock(); r.failure = err }

// DurableDomain declares one producer and maps a concrete model change to its
// event DTO. Foundry owns serialization, generated storage and transaction scope.
// The trace is only an acceptance aid; ordinary consumers can return enqueue's
// error directly without keeping the resulting message ID in memory.
func DurableDomain() foundation.Module {
	return foundation.Module{Name: "domain.durable", Requires: []foundation.ProviderID{"database", "events"}, OnRegister: func(r *foundation.Registrar) error {
		if err := events.RegisterOutbox(r, Durable, Bus, "consumer.domain"); err != nil {
			return err
		}
		if err := events.RegisterTopic(r, Bus, Created); err != nil {
			return err
		}
		if err := foundation.Factory(r, Enqueued, func(foundation.Resolver) (*EnqueueTrace, error) { return &EnqueueTrace{}, nil }); err != nil {
			return err
		}
		return observerqueries.RegisterPlainObserver(r, Pool, observerqueries.NewPlainObserver("record.outbox"), func(s foundation.Resolver) (func() observerqueries.PlainHooks, error) {
			producer, err := foundation.Resolve(s, Durable)
			if err != nil {
				return nil, err
			}
			trace, err := foundation.Resolve(s, Enqueued)
			if err != nil {
				return nil, err
			}
			return func() observerqueries.PlainHooks {
				return observerqueries.PlainHooks{Created: func(ctx context.Context, tx *database.Tx, changes observerqueries.PlainChanges) error {
					after, present := changes.After().Get()
					if !present {
						return errors.New("created observer has no stored snapshot")
					}
					id, err := Created.Enqueue(ctx, tx, producer, RecordCreated{ID: after.ID})
					if err != nil {
						return err
					}
					return trace.Record(id)
				}}
			}, nil
		})
	}}
}
