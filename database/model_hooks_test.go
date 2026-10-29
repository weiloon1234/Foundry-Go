package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestModelHookFactoriesRollbackPanicsAndGoexit(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			state := mutationDriver(nil)
			db := open(t, state, nil)
			q := mutationDefinition(orm.WithHooks(func() orm.Hooks[mutationRecord] {
				if mode == "panic" {
					panic("private factory payload")
				}
				runtime.Goexit()
				return orm.Hooks[mutationRecord]{}
			}))
			got, err := orm.Insert(t.Context(), db, q.Table, orm.TextSet(q.Name, "private record name"))
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
			q := mutationDefinition(orm.WithHooks(func() orm.Hooks[mutationRecord] { calls++; return orm.Hooks[mutationRecord]{} }))
			got, err := orm.Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 1, orm.TextSet(q.Name, "private record name"))
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
			q := mutationDefinition(orm.WithHooks(func() orm.Hooks[mutationRecord] {
				return orm.Hooks[mutationRecord]{
					Saving: func(ctx context.Context, tx *database.Tx, before value.Optional[mutationRecord], _ *orm.Write[mutationRecord]) error {
						if state.rowsClosed.Load() != 1 || !before.IsSet() {
							t.Error("before callback ran with open/missing row")
						}
						return nil
					},
					Updated: func(ctx context.Context, tx *database.Tx, changes orm.Changes[mutationRecord]) error {
						if state.rowsClosed.Load() != 2 || !changes.After().IsSet() {
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
			got, err := orm.Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 42, orm.TextSet(q.Name, "private record name"))
			if outcome == "success" {
				if err != nil || got.ID != 42 || afterCommits != 1 {
					t.Fatalf("successful hook write failed: %v", err)
				}
				return
			}
			var candidate *orm.WriteError[mutationRecord]
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

func TestRegisteredObserversRequireMatchingFamilyAndRunThroughActualTransaction(t *testing.T) {
	for _, mode := range []string{"wrong-family", "panic", "goexit", "before-veto"} {
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
			veto := errors.New("registered veto")
			calls := 0
			var declaration lifecycle.Declaration
			if mode == "wrong-family" {
				// An application-supplied hook family cannot stand in for the native adapter.
				declaration, err = lifecycle.NewObserver[mutationRecord, struct{}]("observer").Declare(func() struct{} { calls++; return struct{}{} })
			} else {
				declaration, err = orm.NewObserver[mutationRecord]("observer").Declare(func() orm.Hooks[mutationRecord] {
					calls++
					switch mode {
					case "panic":
						panic("private observer factory")
					case "goexit":
						runtime.Goexit()
					}
					return orm.Hooks[mutationRecord]{Creating: func(_ context.Context, tx *database.Tx, _ *orm.Write[mutationRecord]) error {
						if !lifecycle.HasObservers[mutationRecord](tx.Observers()) {
							t.Error("adapter lost owning transaction observers")
						}
						return veto
					}}
				})
			}
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
			_, err = orm.Insert(t.Context(), observerTransactor{db}, mutationFields.Table, mutationValues()...)
			want := veto
			if mode == "wrong-family" {
				want = fault.Invalid
			} else if mode == "panic" || mode == "goexit" {
				want = fault.Panicked
			}
			if !errors.Is(err, want) || state.rolledBack.Load() != 1 || state.rowsClosed.Load() != 0 {
				t.Fatal("registered factory escaped transaction or executed SQL after failure", err)
			}
			if (mode == "wrong-family" && calls != 0) || (mode != "wrong-family" && calls != 1) {
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
	// A registration on another pool must not turn this wrapper's write into
	// an observed write or invoke an unrelated factory.
	declaration, err := orm.NewObserver[mutationRecord]("other.pool").Declare(func() orm.Hooks[mutationRecord] {
		t.Error("observer-free adapter invoked")
		return orm.Hooks[mutationRecord]{}
	})
	if err != nil {
		t.Fatal(err)
	}
	otherObservers, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	_ = rowObserverPool(t, mutationDriver(nil), otherObservers)
	// Native adapters are installed even without a local factory. The actual
	// transaction's empty registry must avoid the lifecycle snapshot read.
	if _, err := orm.Update(t.Context(), observerTransactor{db}, mutationFields.Table.Query(), mutationFields.Table.Key(), 42, mutationValues()...); err != nil {
		t.Fatal(err)
	}

	if queries != 1 || state.committed.Load() != 1 {
		t.Fatal("wrapper fallback created extra transaction or query")
	}
}
