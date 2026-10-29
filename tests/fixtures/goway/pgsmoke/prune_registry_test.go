package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Compiled by Lane B; execution belongs to the lead's isolated PostgreSQL gate.
func TestORMPruneRegistryRollbackAndMassAcknowledgement(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	cause := errors.New("reject second deletion")
	deleting, committed := 0, 0
	f := orm.Define("prune_registry_records", func(b orm.Builder[storeRecord], m *storeRecord) storeTable {
		return storeTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	}, orm.WithHooks(func() orm.Hooks[storeRecord] {
		return orm.Hooks[storeRecord]{
			Deleting: func(context.Context, *database.Tx, storeRecord) error {
				deleting++
				if deleting == 2 {
					return cause
				}
				return nil
			},
			AfterCommit: func(_ context.Context, operation lifecycle.Operation, _ orm.Changes[storeRecord]) error {
				if operation == lifecycle.Delete {
					committed++
				}
				return nil
			},
		}
	}))
	_, err := db.Exec(ctx, `CREATE TABLE prune_registry_records(id uuid PRIMARY KEY,name text NOT NULL)`)
	must(t, err)
	for _, name := range []string{"expired", "expired", "retained"} {
		_, err := orm.Insert(ctx, db, f.Table, orm.TextSet(f.Name, name))
		must(t, err)
	}
	selection := f.Query().Where(orm.TextEq(f.Name, "expired"))
	r, err := prune.New(prune.ORMModel("app.records", selection, prune.Lifecycle))
	must(t, err)
	result, err := r.Run(ctx, db, prune.Options{BatchSize: 2, MaxBatches: 1})
	if !errors.Is(err, cause) || len(result.Counts) != 1 || result.Counts[0].Removed != 0 || result.StoppedAt == nil || committed != 0 {
		t.Fatal("failed batch reported committed deletion", result, committed)
	}
	count, err := orm.Count(ctx, db, f.Query())
	must(t, err)
	if count != 3 {
		t.Fatal("failed batch left a partial deletion", count)
	}
	r, err = prune.New(prune.ORMModel("app.records", selection, prune.Mass))
	must(t, err)
	result, err = r.Run(ctx, db, prune.Options{BatchSize: 2, MaxBatches: 1})
	must(t, err)
	if len(result.Counts) != 1 || result.Counts[0].Removed != 2 || !result.Counts[0].Remaining || deleting != 2 || committed != 0 {
		t.Fatal("mass acknowledgement dispatched lifecycle or lost its bound", result, deleting, committed)
	}
	remaining, err := orm.All(ctx, db, f.Query())
	must(t, err)
	if len(remaining) != 1 || remaining[0].Name != "retained" {
		t.Fatal("pruning lost its selection")
	}
}
