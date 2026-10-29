package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type eachSoftRow struct {
	ID        int64
	Name      string
	DeletedAt value.Nullable[time.Time]
}
type eachSoftTable struct {
	Table[eachSoftRow, int64]
	Name Text[eachSoftRow, string]
	Soft SoftDeletable[eachSoftRow]
}

func eachSoftFields(factory func() Hooks[eachSoftRow]) eachSoftTable {
	options := []Option{}
	if factory != nil {
		options = append(options, WithHooks(factory))
	}
	return Define("each_soft", func(b Builder[eachSoftRow], m *eachSoftRow) eachSoftTable {
		deleted := SoftDeleteOf(b, &m.DeletedAt)
		return eachSoftTable{TableOf(b, &m.ID), TextOf(b, &m.Name), SoftDeletes(deleted)}
	}, options...)
}

var eachSoftOperations = []struct {
	name      string
	operation lifecycle.Operation
	run       func(context.Context, database.Transactor, Query[eachSoftRow], SoftDeletable[eachSoftRow], int) ([]eachSoftRow, error)
}{
	{"restore", lifecycle.Restore, RestoreEach[eachSoftRow]},
	{"force_delete", lifecycle.ForceDelete, ForceDeleteEach[eachSoftRow]},
}

func TestEachSoftPreflight(t *testing.T) {
	d, foreign := eachSoftFields(nil), eachSoftFields(nil)
	for _, op := range eachSoftOperations {
		t.Run(op.name, func(t *testing.T) {
			w := &noEachWriter{}
			for _, token := range []SoftDeletable[eachSoftRow]{{}, foreign.Soft} {
				if got, err := op.run(t.Context(), w, d.Query(), token, 1); !errors.Is(err, fault.Invalid) || got != nil {
					t.Fatal(got, err)
				}
			}
			for _, n := range []int{0, -1, MaxPerModelWriteRows + 1} {
				if _, err := op.run(t.Context(), w, d.Query(), d.Soft, n); !errors.Is(err, fault.Invalid) {
					t.Fatal(err)
				}
			}
			for _, q := range []Query[eachSoftRow]{{}, WithoutModelHooks(d.Query()), d.Query().Limit(1), d.Query().OrderBy(KeyAsc(d.Key()))} {
				if _, err := op.run(t.Context(), w, q, d.Soft, 1); !errors.Is(err, fault.Invalid) {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := op.run(ctx, w, d.Query(), d.Soft, 1); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if w.calls != 0 {
				t.Fatal(w.calls)
			}
		})
	}
}
func TestEachSoftLifecycleVisibilityAndOutcomes(t *testing.T) {
	for _, op := range eachSoftOperations {
		for _, visibility := range []string{"active", "trashed", "all"} {
			for _, outcome := range []string{"ok", "unknown", "committed"} {
				t.Run(op.name+"/"+visibility+"/"+outcome, func(t *testing.T) {
					var trace []string
					closed, begins, writes := 0, 0, 0
					instant := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
					var deleted driver.Value
					if op.operation == lifecycle.Restore || visibility != "active" {
						deleted = instant
					}
					makeHooks := func(owner string) Hooks[eachSoftRow] {
						before := func(_ context.Context, _ *database.Tx, m eachSoftRow) error {
							if closed == 0 || m.ID != int64(writes+1) {
								t.Error("candidate rows not closed or identity lost")
							}
							trace = append(trace, owner+".before")
							return nil
						}
						after := func(_ context.Context, _ *database.Tx, c Changes[eachSoftRow]) error {
							before, ok := c.Before().Get()
							if !ok || before.ID != int64(writes) {
								t.Error("missing locked original")
							}
							if operation, ok := c.Operation().Get(); !ok || operation != op.operation {
								t.Error("operation lost")
							}
							if op.operation == lifecycle.Restore {
								after, ok := c.After().Get()
								if !ok || !after.DeletedAt.IsNull() {
									t.Error("restored snapshot missing")
								}
							} else if c.After().IsSet() {
								t.Error("deleted model retained after snapshot")
							}
							trace = append(trace, owner+".after")
							return nil
						}
						h := Hooks[eachSoftRow]{AfterCommit: func(context.Context, lifecycle.Operation, Changes[eachSoftRow]) error {
							trace = append(trace, owner+".commit")
							if outcome == "committed" {
								return errors.New("commit hook failed")
							}
							return nil
						}}
						if op.operation == lifecycle.Restore {
							h.Restoring, h.Restored = before, after
						} else {
							h.ForceDeleting, h.ForceDeleted = before, after
						}
						return h
					}
					d := eachSoftFields(func() Hooks[eachSoftRow] { return makeHooks("local") })
					observer, err := NewObserver[eachSoftRow]("each.soft").Declare(func() Hooks[eachSoftRow] { return makeHooks("observer") })
					if err != nil {
						t.Fatal(err)
					}
					var statements []string
					connector := testConnector{statements: &statements, onClose: func() { closed++ }, onBegin: func() { begins++ }, respond: func(s string) ([]string, [][]driver.Value) {
						cols := []string{"id", "name", "deleted_at"}
						if strings.Contains(s, "ORDER BY") {
							return cols, [][]driver.Value{{int64(1), "a", deleted}, {int64(2), "b", deleted}}
						}
						id := int64(writes + 1)
						value := deleted
						if strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") {
							writes++
							if op.operation == lifecycle.Restore {
								value = nil
							}
						}
						return cols, [][]driver.Value{{id, "stored", value}}
					}}
					if outcome == "unknown" {
						connector.commitErr = errors.New("connection lost")
					}
					db := hookDB(t, connector, observer)
					q := d.Query().Where(TextNe(d.Name, "excluded"))
					if visibility == "trashed" {
						q = q.OnlyTrashed()
					}
					if visibility == "all" {
						q = q.WithTrashed()
					}
					got, err := op.run(t.Context(), db, q, d.Soft, 2)
					if outcome == "ok" {
						if err != nil || len(got) != 2 {
							t.Fatal(got, err)
						}
					} else {
						var reconciliation *WriteError[[]eachSoftRow]
						var databaseErr *database.Error
						expected := database.Unknown
						if outcome == "committed" {
							expected = database.Committed
						}
						if got != nil || !errors.As(err, &reconciliation) || len(reconciliation.Candidate()) != 2 || !errors.As(err, &databaseErr) || databaseErr.Outcome() != expected {
							t.Fatal(got, err)
						}
						got = reconciliation.Candidate()
					}
					if begins != 1 || writes != 2 || got[0].ID != 1 || got[1].ID != 2 {
						t.Fatal(begins, writes, got)
					}
					for _, m := range got {
						if m.DeletedAt.IsNull() != (op.operation == lifecycle.Restore || deleted == nil) {
							t.Fatal("returned state", m)
						}
					}
					want := []string{"local.before", "observer.before", "local.after", "observer.after", "local.before", "observer.before", "local.after", "observer.after"}
					if outcome != "unknown" {
						want = append(want, "local.commit", "observer.commit", "local.commit", "observer.commit")
					}
					if !reflect.DeepEqual(trace, want) {
						t.Fatal(trace)
					}
					for _, s := range statements {
						if !strings.HasPrefix(s, "SELECT") && !strings.HasPrefix(s, "UPDATE") && !strings.HasPrefix(s, "DELETE") {
							continue
						}
						if !strings.Contains(s, `"name" <>`) {
							t.Fatal("predicate lost", s)
						}
						if op.operation == lifecycle.Restore || visibility == "trashed" {
							if !strings.Contains(s, `"deleted_at" IS NOT NULL`) {
								t.Fatal("trashed visibility lost", s)
							}
						} else if visibility == "active" {
							if !strings.Contains(s, `"deleted_at" IS NULL`) {
								t.Fatal("active visibility lost", s)
							}
						} else if strings.Contains(s, `"deleted_at" IS`) {
							t.Fatal("all visibility changed", s)
						}
					}
				})
			}
		}
	}
}
func TestEachSoftFailuresRemainAtomic(t *testing.T) {
	for _, op := range eachSoftOperations {
		for _, mode := range []string{"overflow", "error", "panic", "goexit", "cancel", "after"} {
			t.Run(op.name+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls, writes, commits := 0, 0, 0
				stop := errors.New("stop")
				before := func(context.Context, *database.Tx, eachSoftRow) error {
					calls++
					if calls == 2 {
						switch mode {
						case "error":
							return stop
						case "panic":
							panic("private")
						case "goexit":
							runtime.Goexit()
						case "cancel":
							cancel()
						}
					}
					return nil
				}
				after := func(context.Context, *database.Tx, Changes[eachSoftRow]) error {
					if mode == "after" && writes == 2 {
						return stop
					}
					return nil
				}
				h := Hooks[eachSoftRow]{AfterCommit: func(context.Context, lifecycle.Operation, Changes[eachSoftRow]) error { commits++; return nil }}
				if op.operation == lifecycle.Restore {
					h.Restoring, h.Restored = before, after
				} else {
					h.ForceDeleting, h.ForceDeleted = before, after
				}
				d := eachSoftFields(func() Hooks[eachSoftRow] { return h })
				db := hookDB(t, testConnector{respond: func(s string) ([]string, [][]driver.Value) {
					cols := []string{"id", "name", "deleted_at"}
					instant := time.Now().UTC()
					if strings.Contains(s, "ORDER BY") {
						return cols, [][]driver.Value{{int64(1), "a", instant}, {int64(2), "b", instant}}
					}
					id := int64(writes + 1)
					var deleted driver.Value = instant
					if strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") {
						writes++
						if op.operation == lifecycle.Restore {
							deleted = nil
						}
					}
					return cols, [][]driver.Value{{id, "a", deleted}}
				}})
				limit := 2
				if mode == "overflow" {
					limit = 1
				}
				got, err := op.run(ctx, db, d.Query().OnlyTrashed(), d.Soft, limit)
				want := stop
				switch mode {
				case "overflow":
					want = fault.Invalid
				case "panic", "goexit":
					want = fault.Panicked
				case "cancel":
					want = context.Canceled
				}
				expectedWrites := 1
				if mode == "overflow" {
					expectedWrites = 0
				}
				if mode == "after" {
					expectedWrites = 2
				}
				if got != nil || !errors.Is(err, want) || commits != 0 || writes != expectedWrites {
					t.Fatal(got, err, commits, writes)
				}
				if mode == "overflow" && calls != 0 {
					t.Fatal("overflow invoked hooks")
				}
			})
		}
	}
}
