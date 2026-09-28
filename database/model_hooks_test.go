package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestModelHookFactoriesRollbackPanicsAndGoexit(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			state := mutationDriver(nil)
			db := open(t, state, nil)
			q := query.ForModel(mutationDefinition().WithWriteHooks(func() query.WriteHooks[mutationRecord] {
				if mode == "panic" {
					panic("private factory payload")
				}
				runtime.Goexit()
				return query.WriteHooks[mutationRecord]{}
			}))
			got, err := q.Insert(t.Context(), db, mutationValues())
			if !errors.Is(err, fault.Panicked) || got != (mutationRecord{}) || state.rolledBack.Load() != 1 || state.rowsClosed.Load() != 0 {
				t.Fatalf("factory failure escaped transaction isolation: %v", err)
			}
			if strings.Contains(err.Error(), "private factory payload") {
				t.Fatal("factory panic leaked")
			}
		})
	}
}

func TestModelHooksRejectAmbiguousLockedRowsBeforeCallbacks(t *testing.T) {
	for _, test := range []struct {
		name string
		rows [][]driver.Value
		want error
	}{
		{"missing", nil, database.NotFound},
		{"duplicate", [][]driver.Value{{int64(1), "first"}, {int64(1), "second"}}, database.TooManyRows},
		{"invalid", [][]driver.Value{{"bad id", "value"}}, fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := mutationDriver(test.rows)
			db := open(t, state, nil)
			calls := 0
			q := query.ForModel(mutationDefinition().WithWriteHooks(func() query.WriteHooks[mutationRecord] { calls++; return query.WriteHooks[mutationRecord]{} }))
			id := query.NewScalarField[mutationRecord, int64]("records", "id", codec.Signed[int64]())
			got, err := q.Where(id.Eq(1)).Patch(t.Context(), db, mutationValues())
			if !errors.Is(err, test.want) || got != (mutationRecord{}) || calls != 0 || state.rolledBack.Load() != 1 || state.rowsClosed.Load() != 1 {
				t.Fatalf("invalid locked row ran factory or escaped rollback: %v", err)
			}
		})
	}
}

func TestModelHooksCloseRowsAndPreserveCommitCandidates(t *testing.T) {
	for _, outcome := range []string{"success", "unknown", "callback"} {
		t.Run(outcome, func(t *testing.T) {
			state := mutationDriver([][]driver.Value{{int64(42), "private record name"}})
			if outcome == "unknown" {
				state.commitError = errors.New("lost commit response")
			}
			db := open(t, state, nil)
			queries, afterCommits := 0, 0
			state.query = func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
				queries++
				if queries == 1 && (!strings.Contains(statement, "FOR UPDATE") || !strings.Contains(statement, "LIMIT")) {
					t.Error("before snapshot was not bounded and locked")
				}
				return &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(42), "private record name"}}}, nil
			}
			q := query.ForModel(mutationDefinition().WithWriteHooks(func() query.WriteHooks[mutationRecord] {
				return query.WriteHooks[mutationRecord]{
					Before: func(ctx context.Context, tx *database.Tx, op lifecycle.Operation, before value.Optional[mutationRecord], m query.Mutation[mutationRecord]) (query.Mutation[mutationRecord], error) {
						if state.rowsClosed.Load() != 1 || op != lifecycle.Update || !before.IsSet() {
							t.Error("before callback ran with open/missing row")
						}
						return m, nil
					},
					After: func(ctx context.Context, tx *database.Tx, op lifecycle.Operation, before, after value.Optional[mutationRecord], m query.Mutation[mutationRecord]) error {
						if state.rowsClosed.Load() != 2 || !after.IsSet() {
							t.Error("after callback ran with open/missing result")
						}
						return tx.AfterCommit(func(context.Context) error {
							afterCommits++
							if outcome == "callback" {
								return fault.New(fault.Invalid, "post-commit veto")
							}
							return nil
						})
					},
				}
			}))
			id := query.NewScalarField[mutationRecord, int64]("records", "id", codec.Signed[int64]())
			got, err := q.Where(id.Eq(42)).Patch(t.Context(), db, mutationValues())
			if outcome == "success" {
				if err != nil || got.ID != 42 || afterCommits != 1 {
					t.Fatalf("successful hook write failed: %v", err)
				}
				return
			}
			var candidate *query.WriteError[mutationRecord]
			if got != (mutationRecord{}) || !errors.As(err, &candidate) || candidate.Candidate().ID != 42 {
				t.Fatalf("hook write lost reconciliation candidate: %v", err)
			}
			if outcome == "unknown" && (!errors.Is(err, database.CommitUnknown) || afterCommits != 0) {
				t.Fatal("unconfirmed commit ran callbacks")
			}
			if outcome == "callback" && (!errors.Is(err, database.AfterCommitFailed) || afterCommits != 1) {
				t.Fatal("callback failure lost committed outcome")
			}
			if queries != 2 || state.committed.Load() != 1 || state.rolledBack.Load() != 0 {
				t.Fatal("hook pipeline retried its write")
			}
		})
	}
}

func TestRegisteredObserversRequireAdapterAndRunThroughActualTransaction(t *testing.T) {
	for _, mode := range []string{"missing-adapter", "factory-error", "panic", "goexit", "before-veto"} {
		t.Run(mode, func(t *testing.T) {
			state := mutationDriver([][]driver.Value{{int64(42), "record"}})
			db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			declaration, err := lifecycle.NewObserver[mutationRecord, query.WriteHooks[mutationRecord]]("observer").Declare(func() query.WriteHooks[mutationRecord] { return query.WriteHooks[mutationRecord]{} })
			if err != nil {
				t.Fatal(err)
			}
			set, err := lifecycle.NewObservers(declaration)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.BindObservers(set); err != nil {
				t.Fatal(err)
			}
			if err := db.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			veto := errors.New("registered veto")
			definition := mutationDefinition()
			calls := 0
			if mode != "missing-adapter" {
				definition = definition.WithObserverHooks(func(ctx context.Context, owned lifecycle.Observers) (query.WriteHooks[mutationRecord], error) {
					calls++
					if !lifecycle.HasObservers[mutationRecord](owned) {
						t.Error("adapter lost owning transaction observers")
					}
					switch mode {
					case "factory-error":
						return query.WriteHooks[mutationRecord]{}, veto
					case "panic":
						panic("private observer factory")
					case "goexit":
						runtime.Goexit()
					}
					return query.WriteHooks[mutationRecord]{Before: func(ctx context.Context, tx *database.Tx, _ lifecycle.Operation, _ value.Optional[mutationRecord], _ query.Mutation[mutationRecord]) (query.Mutation[mutationRecord], error) {
						return query.Mutation[mutationRecord]{}, veto
					}}, nil
				}, false)
			}
			_, err = query.ForModel(definition).Insert(t.Context(), observerTransactor{db}, mutationValues())
			want := veto
			if mode == "missing-adapter" {
				want = fault.Invalid
			} else if mode == "panic" || mode == "goexit" {
				want = fault.Panicked
			}
			if !errors.Is(err, want) || state.rolledBack.Load() != 1 || state.rowsClosed.Load() != 0 {
				t.Fatal("registered factory escaped transaction or executed SQL after failure", err)
			}
			if (mode == "missing-adapter" && calls != 0) || (mode != "missing-adapter" && calls != 1) {
				t.Fatal("factory repeated or stale adapter called")
			}
		})
	}
}

func TestObserverFreeWrapperRetainsSingleMutationStatement(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(42), "record"}})
	db := open(t, state, nil)
	queries := 0
	state.query = func(_ context.Context, sql string, _ []driver.NamedValue) (driver.Rows, error) {
		queries++
		if !strings.HasPrefix(sql, "UPDATE ") {
			t.Error("observer-free wrapper added a snapshot read", sql)
		}
		return &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(42), "record"}}}, nil
	}
	d := mutationDefinition().WithObserverHooks(func(context.Context, lifecycle.Observers) (query.WriteHooks[mutationRecord], error) {
		t.Error("observer-free adapter invoked")
		return query.WriteHooks[mutationRecord]{}, nil
	}, false)
	id := query.NewScalarField[mutationRecord, int64]("records", "id", codec.Signed[int64]())
	if _, err := query.ForModel(d).Where(id.Eq(42)).Patch(t.Context(), observerTransactor{db}, mutationValues()); err != nil {
		t.Fatal(err)
	}
	if queries != 1 || state.committed.Load() != 1 {
		t.Fatal("wrapper fallback created extra transaction or query")
	}
}
