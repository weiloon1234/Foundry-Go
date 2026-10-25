package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestRegisteredObserverAfterCommitDiagnostics(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("owned=%t/goexit=%t", owned, exit), func(t *testing.T) {
				input, raw, cause := databaseDiagnosticInput(t, owned, exit)
				state := mutationDriver([][]driver.Value{{int64(42), "private record name"}})
				db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				callbacks := 0
				continued := false
				declaration, err := lifecycle.NewObserver[mutationRecord, query.WriteHooks[mutationRecord]]("diagnostic.observer").Declare(func() query.WriteHooks[mutationRecord] {
					return query.WriteHooks[mutationRecord]{After: func(_ context.Context, tx *database.Tx, _ lifecycle.Operation, _, _ value.Optional[mutationRecord], _ query.Mutation[mutationRecord]) error {
						if err := tx.AfterCommit(func(context.Context) error { callbacks++; return input }); err != nil {
							return err
						}
						return tx.AfterCommit(func(context.Context) error { continued = true; return nil })
					}}
				})
				if err != nil {
					t.Fatal(err)
				}
				observers, err := lifecycle.NewObservers(declaration)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.BindObservers(observers); err != nil {
					t.Fatal(err)
				}
				if err := db.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
				definition := mutationDefinition().WithObserverHooks(func(_ context.Context, set lifecycle.Observers) (query.WriteHooks[mutationRecord], error) {
					factories, err := lifecycle.ObserverFactories[mutationRecord, query.WriteHooks[mutationRecord]](set)
					if err != nil {
						return query.WriteHooks[mutationRecord]{}, err
					}
					if len(factories) != 1 {
						return query.WriteHooks[mutationRecord]{}, errors.New("missing registered observer")
					}
					return factories[0](), nil
				}, false)
				var result mutationRecord
				err = callback.Isolated("consume database result", func() error {
					var err error
					result, err = query.ForModel(definition).Insert(t.Context(), db, mutationValues())
					return err
				})
				innerWant := "after commit: database after commit: after_commit_failed"
				if owned {
					innerWant = "after commit: " + ownedDatabaseDiagnostic
				}
				assertDatabaseDiagnostic(t, err, input, raw, cause, "consume database result: model write requires transaction outcome reconciliation")
				var outcome *database.Error
				var reconciliation *query.WriteError[mutationRecord]
				if !errors.Is(err, database.AfterCommitFailed) || !errors.As(err, &outcome) || outcome.Outcome() != database.Committed || !errors.As(err, &reconciliation) || reconciliation.Candidate().ID != 42 || result != (mutationRecord{}) {
					t.Fatal("observer diagnostic lost the committed outcome or reconciliation candidate")
				}
				if message, owned := safeerror.OwnedMessage(reconciliation.Unwrap()); !owned || message != innerWant {
					t.Fatal("observer lost its retained after-commit diagnostic")
				}
				if callbacks != 1 || !continued || state.committed.Load() != 1 || state.rolledBack.Load() != 0 || state.rowsClosed.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("observer diagnostic changed callback or transaction ownership")
				}
			})
		}
	}
}
