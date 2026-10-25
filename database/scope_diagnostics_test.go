package database_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

const ownedDatabaseDiagnostic = "invalid: cannot decode configuration field entry from fixture"

type privateDatabaseDiagnostic struct {
	exit      bool
	formatted atomic.Int32
}

func (e *privateDatabaseDiagnostic) Error() string {
	e.formatted.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private database diagnostic")
}
func (e *privateDatabaseDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func databaseDiagnosticInput(t *testing.T, owned, exit bool) (error, *fault.Error, *privateDatabaseDiagnostic) {
	t.Helper()
	cause := &privateDatabaseDiagnostic{exit: exit}
	var input error = fault.Wrap(fault.Invalid, "private application diagnostic", cause)
	if owned {
		type settings struct{ Entry string }
		schema, err := config.New(config.NewKey("entry", func(v *settings) *string { return &v.Entry }, func(string) (string, error) { return "", cause }))
		if err != nil {
			t.Fatal(err)
		}
		_, _, input = schema.Load(settings{}, config.Inputs[settings]{Files: []config.Values{{Name: "fixture", Data: map[string]string{"entry": "private input"}}}})
	}
	var raw *fault.Error
	if input == nil || !errors.As(input, &raw) {
		t.Fatal("diagnostic fixture did not produce a fault")
	}
	return input, raw, cause
}

func assertDatabaseDiagnostic(t *testing.T, result, input error, raw *fault.Error, cause *privateDatabaseDiagnostic, want string) {
	t.Helper()
	var retained *privateDatabaseDiagnostic
	var classified *fault.Error
	if result == nil || result.Error() != want || !errors.Is(result, input) || !errors.Is(result, raw) || !errors.Is(result, fault.Invalid) || !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause || !errors.As(result, &classified) || classified != raw {
		t.Fatal("database boundary lost diagnostic, identity or classification")
	}
	*raw = *fault.Wrap(fault.Invalid, "private replacement diagnostic", cause)
	if got := fmt.Sprintf("%v %+v %#v", result, result, result); got != want+" "+want+" "+want || cause.formatted.Load() != 0 {
		t.Fatal("database boundary formatted application text or exposed a mutable diagnostic")
	}
}

func TestSessionAndTransactionBoundaryDiagnostics(t *testing.T) {
	for _, operation := range []string{"session", "transaction", "unconfirmed rollback"} {
		for _, owned := range []bool{false, true} {
			for _, exit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/owned=%t/goexit=%t", operation, owned, exit), func(t *testing.T) {
					input, raw, cause := databaseDiagnosticInput(t, owned, exit)
					state := &driverState{}
					cleanup := errors.New("private rollback failure")
					if operation == "unconfirmed rollback" {
						state.rollbackError = cleanup
					}
					db := open(t, state, nil)
					var returned error
					err := callback.Isolated("consume database result", func() error {
						if operation == "session" {
							returned = db.Session(t.Context(), func(*database.Session) error { return input })
						} else {
							returned = db.Transaction(t.Context(), func(*database.Tx) error { return input })
						}
						return returned
					})
					want := "consume database result failed"
					if operation == "unconfirmed rollback" {
						want = "consume database result: operation failed\ndatabase rollback: query_failed"
					}
					if owned {
						want = "consume database result: " + ownedDatabaseDiagnostic
						if operation == "unconfirmed rollback" {
							want += "\ndatabase rollback: query_failed"
						}
					}
					assertDatabaseDiagnostic(t, err, input, raw, cause, want)
					var outcome *database.Error
					if operation == "unconfirmed rollback" {
						if !errors.Is(err, cleanup) || !errors.As(err, &outcome) || outcome.Outcome() != database.NoCommit {
							t.Fatal("diagnostic preservation hid rollback uncertainty")
						}
					} else if returned != input || errors.As(err, &outcome) {
						t.Fatal("clean scope changed callback identity or added a database outcome")
					}
					if state.committed.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
						t.Fatal("failed scope committed or retained ownership")
					}
					if operation == "session" && state.closed.Load() != 1 || operation != "session" && state.rolledBack.Load() != 1 {
						t.Fatal("scope cleanup did not finish")
					}
				})
			}
		}
	}
}

func TestAfterCommitBoundaryDiagnostics(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("owned=%t/goexit=%t", owned, exit), func(t *testing.T) {
				input, raw, cause := databaseDiagnosticInput(t, owned, exit)
				state := &driverState{}
				db := open(t, state, nil)
				continued := false
				err := callback.Isolated("consume database result", func() error {
					return db.Transaction(t.Context(), func(tx *database.Tx) error {
						if err := tx.AfterCommit(func(context.Context) error { return input }); err != nil {
							return err
						}
						return tx.AfterCommit(func(context.Context) error { continued = true; return nil })
					})
				})
				want := "consume database result: after commit: database after commit: after_commit_failed"
				if owned {
					want = "consume database result: after commit: " + ownedDatabaseDiagnostic
				}
				assertDatabaseDiagnostic(t, err, input, raw, cause, want)
				var outcome *database.Error
				if !errors.Is(err, database.AfterCommitFailed) || !errors.As(err, &outcome) || outcome.Outcome() != database.Committed || !continued || state.committed.Load() != 1 || state.rolledBack.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("after-commit diagnostic changed the committed outcome or callback ownership")
				}
			})
		}
	}
}
