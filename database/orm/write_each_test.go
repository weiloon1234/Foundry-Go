package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type eachRow struct {
	ID   int64
	Name string
}
type eachTable struct {
	Table[eachRow, int64]
	Name Text[eachRow, string]
}

func eachFields(h Hooks[eachRow]) eachTable {
	return Define("each_records", func(b Builder[eachRow], r *eachRow) eachTable {
		return eachTable{TableOf(b, &r.ID), TextOf(b, &r.Name)}
	}, WithHooks(func() Hooks[eachRow] { return h }))
}

type noEachWriter struct{ calls int }

func (w *noEachWriter) Transaction(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
	w.calls++
	return errors.New("unexpected transaction")
}
func TestEachWritePreflight(t *testing.T) {
	d := eachFields(Hooks[eachRow]{})
	w := &noEachWriter{}
	for _, n := range []int{-1, 0, MaxPerModelWriteRows + 1} {
		if _, err := DeleteEach(t.Context(), w, d.Query(), n); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	for _, q := range []Query[eachRow]{{}, d.Query().OrderBy(KeyAsc(d.Key())), d.Query().Limit(0), d.Query().Offset(1), WithoutModelHooks(d.Query()), WithRelationLimits(d.Query(), DefaultRelationLimits())} {
		if _, err := DeleteEach(t.Context(), w, q, 1); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := UpdateEach(t.Context(), w, d.Query(), 1, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := DeleteEach(ctx, w, d.Query(), 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var nilDB *database.DB
	if _, err := DeleteEach(t.Context(), nilDB, d.Query(), 1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if w.calls != 0 {
		t.Fatal(w.calls)
	}
}
func TestEachWriteBoundsAndIdentityBeforeCallbacks(t *testing.T) {
	for _, test := range []struct {
		name string
		rows [][]driver.Value
		want error
	}{
		{"overflow", [][]driver.Value{{int64(1), "a"}, {int64(2), "b"}}, fault.Invalid},
		{"duplicate", [][]driver.Value{{int64(1), "a"}, {int64(1), "b"}}, database.TooManyRows},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			d := eachFields(Hooks[eachRow]{Updating: func(context.Context, *database.Tx, eachRow, *Write[eachRow]) error { calls++; return nil }})
			var statements []string
			db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "name"}, values: test.rows})
			limit := 2
			if test.name == "overflow" {
				limit = 1
			}
			_, err := UpdateEach(t.Context(), db, d.Query(), limit, func(context.Context, *database.Tx, eachRow) ([]Assignment[eachRow], error) {
				calls++
				return []Assignment[eachRow]{TextSet(d.Name, "x")}, nil
			})
			if !errors.Is(err, test.want) || calls != 0 || len(statements) != 1 {
				t.Fatal(err, calls, statements)
			}
		})
	}
}
func TestEachWriteLifecycleAndNoPerRowSavepoints(t *testing.T) {
	var trace []string
	closed := 0
	var batchTx *database.Tx
	d := eachFields(Hooks[eachRow]{
		Updating: func(_ context.Context, tx *database.Tx, m eachRow, w *Write[eachRow]) error {
			if tx != batchTx {
				t.Error("hook transaction differs from draft transaction")
			}
			if closed == 0 {
				t.Error("unclosed candidates")
			}
			trace = append(trace, "updating")
			return nil
		},
		Updated: func(context.Context, *database.Tx, Changes[eachRow]) error {
			trace = append(trace, "updated")
			return nil
		},
		AfterCommit: func(context.Context, lifecycle.Operation, Changes[eachRow]) error {
			trace = append(trace, "commit")
			return nil
		},
	})
	n := 0
	var statements []string
	db := testDB(t, testConnector{statements: &statements, onClose: func() { closed++ }, respond: func(s string) ([]string, [][]driver.Value) {
		if strings.Contains(s, "ORDER BY") {
			return []string{"id", "name"}, [][]driver.Value{{int64(1), "a"}, {int64(2), "b"}}
		}
		n++
		id := int64((n + 1) / 2)
		name := "original"
		if strings.HasPrefix(s, "UPDATE") {
			name = "changed"
		}
		return []string{"id", "name"}, [][]driver.Value{{id, name}}
	}})
	var got []eachRow
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		var err error
		got, err = UpdateEach(t.Context(), tx, d.Query().Where(TextNe(d.Name, "excluded")), 2, func(ctx context.Context, actual *database.Tx, m eachRow) ([]Assignment[eachRow], error) {
			if actual == nil || (batchTx != nil && actual != batchTx) {
				t.Error("batch transaction replaced")
			}
			batchTx = actual
			trace = append(trace, "draft")
			return []Assignment[eachRow]{TextSet(d.Name, "changed")}, nil
		})
		if len(trace) != 6 {
			t.Fatal(trace)
		}
		return err
	})
	if err != nil || len(got) != 2 || got[1].ID != 2 {
		t.Fatal(got, err)
	}
	if !reflect.DeepEqual(trace, []string{"draft", "updating", "updated", "draft", "updating", "updated", "commit", "commit"}) {
		t.Fatal(trace)
	}
	saves := 0
	for _, s := range statements {
		if strings.HasPrefix(s, "SAVEPOINT") {
			saves++
		}
		if strings.HasPrefix(s, "UPDATE") && !strings.Contains(s, "\"name\" <>") {
			t.Fatal("scope lost", s)
		}
	}
	if saves != 1 {
		t.Fatal("expected one enclosing savepoint", statements)
	}
}
func TestEachWriteFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "cancel", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			commits := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d := eachFields(Hooks[eachRow]{AfterCommit: func(context.Context, lifecycle.Operation, Changes[eachRow]) error { commits++; return nil }})
			foreign := eachFields(Hooks[eachRow]{})
			var statements []string
			calls := 0
			db := testDB(t, testConnector{statements: &statements, respond: func(s string) ([]string, [][]driver.Value) {
				if strings.Contains(s, "ORDER BY") {
					return []string{"id", "name"}, [][]driver.Value{{int64(1), "a"}, {int64(2), "b"}}
				}
				return []string{"id", "name"}, [][]driver.Value{{int64(1), "x"}}
			}})
			rows, err := UpdateEach(ctx, db, d.Query(), 2, func(context.Context, *database.Tx, eachRow) ([]Assignment[eachRow], error) {
				calls++
				if calls == 2 {
					switch mode {
					case "error":
						return nil, errors.New("stop")
					case "panic":
						panic("private")
					case "goexit":
						runtime.Goexit()
					case "cancel":
						cancel()
					case "foreign":
						return []Assignment[eachRow]{TextSet(foreign.Name, "x")}, nil
					}
				}
				return []Assignment[eachRow]{TextSet(d.Name, "x")}, nil
			})
			if err == nil || rows != nil || commits != 0 || calls != 2 {
				t.Fatal(rows, err, commits, calls)
			}
			updates := 0
			for _, s := range statements {
				if strings.HasPrefix(s, "UPDATE") {
					updates++
				}
			}
			if updates != 1 {
				t.Fatal(statements)
			}
		})
	}
}
func TestEachWriteUnknownOutcome(t *testing.T) {
	d := eachFields(Hooks[eachRow]{})
	begins := 0
	db := testDB(t, testConnector{onBegin: func() { begins++ }, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "a"}}, commitErr: errors.New("connection lost")})
	got, err := DeleteEach(t.Context(), db, d.Query(), 1)
	var writeErr *WriteError[[]eachRow]
	if got != nil || !errors.As(err, &writeErr) || len(writeErr.Candidate()) != 1 || begins != 1 {
		t.Fatal(got, err, begins)
	}
	var outcome *database.Error
	if !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown {
		t.Fatal(err)
	}
}
func TestEachWriteEmptyAndNoRetrieved(t *testing.T) {
	retrieved := 0
	d := Define("empty_records", func(b Builder[eachRow], m *eachRow) eachTable {
		return eachTable{TableOf(b, &m.ID), TextOf(b, &m.Name)}
	}, WithRetrievalHooks(func() RetrievalHooks[eachRow] {
		return RetrievalHooks[eachRow]{Retrieved: func(context.Context, database.Executor, eachRow) error { retrieved++; return nil }}
	}))
	for _, data := range [][][]driver.Value{nil, {{int64(1), "a"}}} {
		db := testDB(t, testConnector{columns: []string{"id", "name"}, values: data})
		got, err := DeleteEach(t.Context(), db, d.Query(), 1)
		if err != nil || got == nil || len(got) != len(data) || retrieved != 0 {
			t.Fatal(got, err, retrieved)
		}
	}
}

func TestEachWriteRegisteredObserversAndCommittedFailure(t *testing.T) {
	for _, failCommitHook := range []bool{false, true} {
		d := Define("observed_each", func(b Builder[eachRow], r *eachRow) eachTable {
			return eachTable{TableOf(b, &r.ID), TextOf(b, &r.Name)}
		})
		var trace []string
		decl, err := NewObserver[eachRow]("each.observer").Declare(func() Hooks[eachRow] {
			return Hooks[eachRow]{
				Deleting: func(context.Context, *database.Tx, eachRow) error { trace = append(trace, "deleting"); return nil },
				Deleted: func(context.Context, *database.Tx, Changes[eachRow]) error {
					trace = append(trace, "deleted")
					return nil
				},
				AfterCommit: func(context.Context, lifecycle.Operation, Changes[eachRow]) error {
					trace = append(trace, "commit")
					if failCommitHook {
						return errors.New("after commit failed")
					}
					return nil
				},
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		db := hookDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "a"}}}, decl)
		got, err := DeleteEach(t.Context(), db, d.Query(), 1)
		if !reflect.DeepEqual(trace, []string{"deleting", "deleted", "commit"}) {
			t.Fatal(trace)
		}
		if !failCommitHook {
			if err != nil || len(got) != 1 {
				t.Fatal(got, err)
			}
			continue
		}
		var writeErr *WriteError[[]eachRow]
		var outcome *database.Error
		if got != nil || !errors.As(err, &writeErr) || len(writeErr.Candidate()) != 1 || !errors.As(err, &outcome) || outcome.Outcome() != database.Committed {
			t.Fatal(got, err)
		}
	}
}

func TestEachWriteRejectsIncomingLockState(t *testing.T) {
	d := eachFields(Hooks[eachRow]{})
	for _, locked := range []LockedQuery[eachRow]{Lock(d.Query()), Lock(d.Query()).SkipLocked(), Lock(d.Query()).NoWait()} {
		// Reconstruct the state formerly reachable by converting LockedQuery to Query.
		// This keeps the regression valid after the phase layouts become distinct.
		for _, behaviorOnly := range []bool{false, true} {
			q := Query[eachRow]{state: locked.state}
			if behaviorOnly {
				q.state.lock = 0
				if q.state.behavior == 0 {
					continue
				}
			}
			w := &noEachWriter{}
			callbacks := 0
			got, err := UpdateEach(t.Context(), w, q, 1, func(context.Context, *database.Tx, eachRow) ([]Assignment[eachRow], error) {
				callbacks++
				return []Assignment[eachRow]{TextSet(d.Name, "changed")}, nil
			})
			if !errors.Is(err, fault.Invalid) || got != nil {
				t.Fatal(got, err)
			}
			got, err = DeleteEach(t.Context(), w, q, 1)
			if !errors.Is(err, fault.Invalid) || got != nil || w.calls != 0 || callbacks != 0 {
				t.Fatal(got, err, w.calls, callbacks)
			}
		}
	}
}
