package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
				declaration, err := orm.NewObserver[mutationRecord]("diagnostic.observer").Declare(func() orm.Hooks[mutationRecord] {
					return orm.Hooks[mutationRecord]{Created: func(_ context.Context, tx *database.Tx, _ orm.Changes[mutationRecord]) error {
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

				var result mutationRecord
				err = callback.Isolated("consume database result", func() error {
					var err error
					result, err = mutationFields.Insert(t.Context(), db, mutationValues()...)
					return err
				})
				innerWant := "after commit: database after commit: after_commit_failed"
				if owned {
					innerWant = "after commit: " + ownedDatabaseDiagnostic
				}
				assertDatabaseDiagnostic(t, err, input, raw, cause, "consume database result: model write requires transaction outcome reconciliation")
				var outcome *database.Error
				var reconciliation *orm.WriteError[mutationRecord]
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
