// Package eventqueries exercises typed model events through production assembly.
package eventqueries

import (
	"context"
	"errors"
	"sync"

	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RecordCreated is a declared domain DTO, independent of the persisted model.
type RecordCreated struct {
	ID model.ID[observerqueries.Plain] `json:"id"`
}

var Created = events.Define[RecordCreated]("records.created", 1)
var Bus = foundation.NewKey[*events.Bus]("consumer.events")
var Pool = foundation.NewKey[*database.DB]("consumer.events.pool")
var Journal = foundation.NewKey[*Log]("consumer.events.log")

type Entry struct {
	Event  RecordCreated
	Origin attribution.Origin
}
type Log struct {
	mu      sync.Mutex
	entries []Entry
	failure error
}

func (l *Log) Handle(ctx context.Context, event RecordCreated) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, Entry{Event: event, Origin: attribution.FromContext(ctx)})
	return l.failure
}
func (l *Log) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Entry(nil), l.entries...)
}
func (l *Log) Reject(err error) { l.mu.Lock(); defer l.mu.Unlock(); l.failure = err }

// Domain connects concrete model observers and event handlers. Foundry owns
// listener assembly, payload/origin capture, savepoints and commit dispatch.
func Domain() foundation.Module {
	return foundation.Module{Name: "domain.events", Requires: []foundation.ProviderID{"database", "events"}, OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Factory(r, Journal, func(foundation.Resolver) (*Log, error) { return &Log{}, nil }); err != nil {
			return err
		}
		if err := events.RegisterListener(r, Bus, Created, "journal", func(s foundation.Resolver) (events.Handler[RecordCreated], error) {
			log, err := foundation.Resolve(s, Journal)
			if err != nil {
				return nil, err
			}
			return log.Handle, nil
		}); err != nil {
			return err
		}
		return observerqueries.RegisterPlainObserver(r, Pool, observerqueries.NewPlainObserver("record.events"), func(s foundation.Resolver) (func() observerqueries.PlainHooks, error) {
			bus, err := foundation.Resolve(s, Bus)
			if err != nil {
				return nil, err
			}
			return func() observerqueries.PlainHooks {
				return observerqueries.PlainHooks{Created: func(ctx context.Context, tx *database.Tx, changes observerqueries.PlainChanges) error {
					after, present := changes.After().Get()
					if !present {
						return errors.New("created observer has no stored snapshot")
					}
					return Created.AfterCommit(ctx, tx, bus, RecordCreated{ID: after.ID})
				}}
			}, nil
		})
	}}
}
