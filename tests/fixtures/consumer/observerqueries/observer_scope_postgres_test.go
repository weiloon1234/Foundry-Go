package observerqueries_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Expose only the ordinary executor methods, preserving the returned Rows.
type observerScopeReader struct{ database.Executor }

// This exercises the runtime adapter boundary. Generated retrieval dispatch is
// still a separate integration; the test invokes the observer scope explicitly.
func TestPostgresObserverScopeAllowsTypedCallbackIOOnOneConnection(t *testing.T) {
	db := pgtest.Open(t, func(config *postgres.Config) {
		config.Pool.MaxOpen, config.Pool.MaxIdle = 1, 1
		config.Pool.AcquireTimeout = time.Second
	})
	namespace := pgtest.Namespace(t, db)
	if err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `CREATE TABLE observed_effects(id uuid PRIMARY KEY,label text NOT NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"transaction", "savepoint"} {
		for _, veto := range []bool{false, true} {
			name := kind + "/commit"
			if veto {
				name = kind + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				cause := errors.New("retrieval callback veto")
				var retained context.Context
				read := func(tx *database.Tx) error {
					rows, err := (observerScopeReader{tx}).Query(t.Context(), "SELECT $1::integer", 42)
					if err != nil {
						return err
					}
					defer rows.Close()
					return rows.WithObserverScope(t.Context(), func(ctx context.Context) error {
						retained = ctx
						var values []int
						for rows.Next() {
							var value int
							if err := rows.Scan(&value); err != nil {
								return err
							}
							values = append(values, value)
						}
						if err := rows.Close(); err != nil {
							return err
						}
						if len(values) != 1 || values[0] != 42 || ctx.Err() != nil {
							return errors.New("stored hydration failed or closing rows canceled observer work")
						}
						q := observerqueries.QueryObservedEffects()
						created, err := q.Create(ctx, tx, observerqueries.EffectDraft{}.SetLabel(name))
						if err != nil {
							return err
						}
						loaded, err := q.Find(ctx, tx, created.ID)
						item, found := loaded.Get()
						if err != nil || !found || item != created {
							return errors.Join(errors.New("typed observer I/O did not share its transaction"), err)
						}
						if veto {
							return cause
						}
						return nil
					})
				}
				err := inSchema(t, db, namespace, func(tx *database.Tx) error {
					if kind == "savepoint" {
						return tx.Savepoint(t.Context(), read)
					}
					return read(tx)
				})
				if veto && !errors.Is(err, cause) || !veto && err != nil {
					t.Fatal("observer callback outcome changed", err)
				}
				if retained == nil || !errors.Is(retained.Err(), context.Canceled) || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("observer retained its context or database resources")
				}
				if err := inSchema(t, db, namespace, func(tx *database.Tx) error {
					items, err := observerqueries.QueryObservedEffects().All(t.Context(), tx)
					if err != nil {
						return err
					}
					var count int
					for _, item := range items {
						if item.Label == name {
							count++
						}
					}
					want := 1
					if veto {
						want = 0
					}
					if count != want {
						return errors.New("observer side effects escaped the owning transaction outcome")
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
