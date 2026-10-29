package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type writeParent struct {
	ID    int64
	Code  int64
	Links relation.Through[writeTarget, writePivot]
}
type writeTarget struct {
	ID   int64
	Code int64
}
type writePivot struct {
	ID        int64
	ParentID  int64
	TargetID  int64
	Role      string
	DeletedAt value.Nullable[temporal.DateTime]
}
type writeParentTable struct {
	orm.Table[writeParent, int64]
	ID    orm.Key[writeParent, int64]
	Code  orm.Number[writeParent, int64]
	Links orm.Through[writeParent, writeTarget, writePivot]
}
type writeTargetTable struct {
	orm.Table[writeTarget, int64]
	ID   orm.Key[writeTarget, int64]
	Code orm.Number[writeTarget, int64]
}
type writePivotTable struct {
	orm.Table[writePivot, int64]
	ID        orm.Key[writePivot, int64]
	ParentID  orm.Number[writePivot, int64]
	TargetID  orm.Number[writePivot, int64]
	Role      orm.Text[writePivot, string]
	DeletedAt orm.SoftDelete[writePivot, temporal.DateTime]
}

func writeRelationTables(opts ...orm.Option) (writeParentTable, writeTargetTable, writePivotTable, orm.ThroughRelation[writeParent, writeTarget, writePivot]) {
	a := orm.Define("write_parents", func(b orm.Builder[writeParent], m *writeParent) writeParentTable {
		t := orm.TableOf(b, &m.ID)
		return writeParentTable{t, t.Key(), orm.NumberOf(b, &m.Code), orm.ThroughOf(b, &m.Links)}
	})
	z := orm.Define("write_targets", func(b orm.Builder[writeTarget], m *writeTarget) writeTargetTable {
		t := orm.TableOf(b, &m.ID)
		return writeTargetTable{t, t.Key(), orm.NumberOf(b, &m.Code)}
	})
	p := orm.Define("write_pivots", func(b orm.Builder[writePivot], m *writePivot) writePivotTable {
		t := orm.TableOf(b, &m.ID)
		return writePivotTable{t, t.Key(), orm.NumberOf(b, &m.ParentID), orm.NumberOf(b, &m.TargetID), orm.TextOf(b, &m.Role), orm.SoftDeleteOf(b, &m.DeletedAt)}
	}, opts...)
	return a, z, p, orm.ManyToManyOf(a.Links, orm.KeyExpr(a.ID), orm.NumberExpr(p.ParentID), orm.NumberExpr(p.TargetID), orm.KeyExpr(z.ID))
}
func pivotTestRows(n int) [][]driver.Value {
	out := make([][]driver.Value, n)
	for i := range out {
		out[i] = []driver.Value{int64(100 + i), int64(1), int64(10 + i), "member", nil}
	}
	return out
}
func pivotTestReply(n int) func(string) ([]string, [][]driver.Value) {
	return func(s string) ([]string, [][]driver.Value) {
		if strings.HasPrefix(s, "SELECT EXISTS(") {
			return []string{"exists"}, [][]driver.Value{{true}}
		}
		if strings.HasPrefix(s, "SELECT COUNT(*) FROM (") {
			return []string{"count"}, [][]driver.Value{{int64(n)}}
		}
		if strings.Contains(s, `FROM "write_parents"`) {
			return make([]string, 2), [][]driver.Value{{int64(1), int64(91)}}
		}
		if strings.Contains(s, `FROM "write_targets"`) {
			rows := make([][]driver.Value, n)
			for i := range rows {
				rows[i] = []driver.Value{int64(10 + i), int64(81 + i)}
			}
			return make([]string, 2), rows
		}
		return make([]string, 5), pivotTestRows(n)
	}
}
func TestRelationWriteLifecycleAndStatements(t *testing.T) {
	for _, mode := range []string{"plain", "hooks", "observers", "deletion_observers"} {
		t.Run(mode, func(t *testing.T) {
			var events []string
			closed := 0
			inserted := 0
			var p writePivotTable
			hooks := func() orm.Hooks[writePivot] {
				return orm.Hooks[writePivot]{
					Creating: func(_ context.Context, _ *database.Tx, w *orm.Write[writePivot]) error {
						if closed < 2 {
							t.Fatal("endpoints still open")
						}
						if v, ok := orm.WriteGet(w, func() func(arg0 int64) orm.Assignment[writePivot] {
							receiver := p.ParentID
							return func(arg0 int64) orm.Assignment[writePivot] { return orm.NumberSet(receiver, arg0) }
						}()).Get(); !ok || v != 1 {
							t.Fatal("missing refreshed source default")
						}
						events = append(events, "creating")
						return nil
					},
					Created: func(context.Context, *database.Tx, orm.Changes[writePivot]) error {
						events = append(events, "created")
						return nil
					},
					AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[writePivot]) error {
						events = append(events, "commit")
						return nil
					},
				}
			}
			var opts []orm.Option
			if mode == "hooks" {
				opts = append(opts, orm.WithHooks(hooks))
			}
			_, _, p0, r := writeRelationTables(opts...)
			p = p0
			var declarations []lifecycle.Declaration
			if mode == "observers" || mode == "deletion_observers" {
				observer := orm.NewObserver[writePivot]("pivot")
				if mode == "deletion_observers" {
					observer = orm.NewDeletionObserver[writePivot]("pivot")
				}
				d, e := observer.Declare(hooks)
				if e != nil {
					t.Fatal(e)
				}
				declarations = append(declarations, d)
			}
			var statements []string
			reply := pivotTestReply(2)
			db := hookDB(t, testConnector{statements: &statements, onClose: func() { closed++ }, respond: func(s string) ([]string, [][]driver.Value) {
				if strings.HasPrefix(s, "INSERT") {
					inserted++
					if mode != "plain" {
						return make([]string, 5), [][]driver.Value{pivotTestRows(2)[inserted-1]}
					}
				}
				return reply(s)
			}}, declarations...)
			out, err := orm.AttachMany(t.Context(), db, r, writeParent{ID: 1, Code: 999}, []writeTarget{{ID: 10}, {ID: 11}}, orm.KeySet(p.ID, 100), orm.TextSet(p.Role, "member"))
			if err != nil || len(out) != 2 {
				t.Fatal(out, err)
			}
			want := 4
			if mode != "plain" {
				want = 5
			}
			if len(statements) != want {
				t.Fatal("statements", len(statements), want, statements)
			}
			if mode == "hooks" || mode == "observers" {
				if fmt.Sprint(events) != "[creating created creating created commit commit]" {
					t.Fatal(events)
				}
			} else if len(events) != 0 {
				t.Fatal(events)
			}
			for _, s := range statements {
				if strings.Contains(s, "SAVEPOINT") {
					t.Fatal("per-row savepoint", s)
				}
			}
		})
	}
}
func TestRelationWriteUnknownOutcomeCandidates(t *testing.T) {
	_, _, p, r := writeRelationTables()
	db := hookDB(t, testConnector{commitErr: errors.New("lost commit acknowledgement"), respond: pivotTestReply(1)})
	out, err := orm.Attach(t.Context(), db, r, writeParent{ID: 1}, writeTarget{ID: 10}, orm.KeySet(p.ID, 100), orm.TextSet(p.Role, "member"))
	var candidate *orm.WriteError[writePivot]
	var outcome *database.Error
	if out.ID != 0 || !errors.As(err, &candidate) || candidate.Candidate().ID != 100 || !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown {
		t.Fatal(out, err)
	}
	removed, err := orm.Detach(t.Context(), db, r, writeParent{ID: 1}, writeTarget{ID: 10})
	var many *orm.WriteError[[]writePivot]
	if removed != nil || !errors.As(err, &many) || len(many.Candidate()) != 1 {
		t.Fatal(removed, err)
	}
	changes, err := orm.Sync(t.Context(), db, r, writeParent{ID: 1}, []writeTarget{{ID: 10}}, nil, []orm.Assignment[writePivot]{orm.TextSet(p.Role, "updated")})
	var combined *orm.WriteError[orm.PivotChanges[writePivot]]
	if len(changes.Updated) != 0 || !errors.As(err, &combined) || len(combined.Candidate().Updated) != 1 {
		t.Fatal(changes, err)
	}
}
func TestRelationWriteValidationAndFailures(t *testing.T) {
	a, z, p, r := writeRelationTables()
	_, _, other, _ := writeRelationTables()
	for name, relation := range map[string]orm.ThroughRelation[writeParent, writeTarget, writePivot]{"zero": {}, "order": r.OrderBy(orm.KeyAsc(z.ID)), "pivot_order": r.OrderByPivot(orm.KeyAsc(p.ID)), "limit": orm.WithRelationWriteLimit(r, 0), "same_key": orm.ManyToManyOf(a.Links, orm.KeyExpr(a.ID), orm.NumberExpr(p.ParentID), orm.NumberExpr(p.ParentID), orm.KeyExpr(z.ID))} {
		t.Run(name, func(t *testing.T) {
			n := 0
			db := hookDB(t, testConnector{onBegin: func() { n++ }, respond: pivotTestReply(1)})
			_, err := orm.Attach(t.Context(), db, relation, writeParent{}, writeTarget{}, orm.KeySet(p.ID, 100))
			if !errors.Is(err, fault.Invalid) || n != 0 {
				t.Fatal(err, n)
			}
		})
	}
	for _, failure := range []string{"assignment_owner", "missing_source", "missing_target", "duplicate_target", "postcondition", "limit", "callback", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			var sql []string
			ctx := t.Context()
			relation := r
			assignment := orm.TextSet(p.Role, "member")
			reply := pivotTestReply(1)
			if failure == "assignment_owner" {
				assignment = orm.TextSet(other.Role, "member")
			}
			if failure == "callback" {
				_, _, p1, r1 := writeRelationTables(orm.WithHooks(func() orm.Hooks[writePivot] {
					return orm.Hooks[writePivot]{Creating: func(context.Context, *database.Tx, *orm.Write[writePivot]) error { return errors.New("veto") }}
				}))
				relation = r1
				assignment = orm.TextSet(p1.Role, "member")
			}
			if failure == "cancel" {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			db := hookDB(t, testConnector{statements: &sql, respond: func(s string) ([]string, [][]driver.Value) {
				cols, rows := reply(s)
				if failure == "missing_source" && strings.Contains(s, `FROM "write_parents"`) || failure == "missing_target" && strings.Contains(s, `FROM "write_targets"`) {
					rows = nil
				}
				if failure == "duplicate_target" && strings.Contains(s, `FROM "write_targets"`) {
					rows = append(rows, rows[0])
				}
				if failure == "postcondition" && strings.HasPrefix(s, "SELECT EXISTS(") {
					rows = [][]driver.Value{{false}}
				}
				return cols, rows
			}})
			var err error
			if failure == "limit" {
				_, err = orm.AttachMany(ctx, db, orm.WithRelationWriteLimit(relation, 1), writeParent{ID: 1}, []writeTarget{{ID: 10}, {ID: 11}}, assignment)
			} else {
				_, err = orm.Attach(ctx, db, relation, writeParent{ID: 1}, writeTarget{ID: 10}, assignment)
			}
			if err == nil {
				t.Fatal("accepted", failure)
			}
			var candidate *orm.WriteError[writePivot]
			if errors.As(err, &candidate) {
				t.Fatal("failed operation exposed candidate")
			}
			if failure != "postcondition" {
				for _, s := range sql {
					if strings.HasPrefix(s, "INSERT") {
						t.Fatal("failure wrote pivot", failure)
					}
				}
			}
		})
	}
}

func TestRelationWriteSavepointsOwnAfterCommit(t *testing.T) {
	commits := 0
	_, _, p, r := writeRelationTables(orm.WithHooks(func() orm.Hooks[writePivot] {
		return orm.Hooks[writePivot]{AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[writePivot]) error { commits++; return nil }}
	}))
	var statements []string
	db := hookDB(t, testConnector{statements: &statements, respond: pivotTestReply(1)})
	rollback := errors.New("rollback outer")
	for _, fail := range []bool{true, false} {
		before := commits
		statements = nil
		err := db.Transaction(t.Context(), func(tx *database.Tx) error {
			for range 2 {
				if _, e := orm.Attach(t.Context(), tx, r, writeParent{ID: 1}, writeTarget{ID: 10}, orm.KeySet(p.ID, 100), orm.TextSet(p.Role, "member")); e != nil {
					return e
				}
			}
			if commits != before {
				t.Fatal("after-commit ran inside outer transaction")
			}
			if fail {
				return rollback
			}
			return nil
		})
		if fail {
			if !errors.Is(err, rollback) || commits != before {
				t.Fatal(err, commits)
			}
		} else if err != nil || commits != before+2 {
			t.Fatal(err, commits)
		}
		n := 0
		for _, s := range statements {
			if strings.HasPrefix(s, "SAVEPOINT ") {
				n++
			}
		}
		if n != 2 {
			t.Fatal("one savepoint per operation", n, statements)
		}
	}
}
func TestRelationWriteRefreshesStoredKeysAndBoundsResults(t *testing.T) {
	a, z, p, _ := writeRelationTables()
	r := orm.ManyToManyOf(a.Links, orm.NumberExpr(a.Code), orm.NumberExpr(p.ParentID), orm.NumberExpr(p.TargetID), orm.NumberExpr(z.Code))
	var statements []string
	var binds [][]any
	db := hookDB(t, testConnector{statements: &statements, bindings: &binds, respond: pivotTestReply(1)})
	_, err := orm.Attach(t.Context(), db, r, writeParent{ID: 1, Code: 999}, writeTarget{ID: 10, Code: 888}, orm.KeySet(p.ID, 100), orm.TextSet(p.Role, "member"))
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 5 {
		t.Fatal(statements)
	}
	if fmt.Sprint(binds[3]) != "[100 91 81 member]" {
		t.Fatal("stale keys used", binds)
	}
	_, _, p, r = writeRelationTables()
	db = hookDB(t, testConnector{respond: func(s string) ([]string, [][]driver.Value) {
		if strings.HasPrefix(s, "UPDATE") {
			return make([]string, 5), pivotTestRows(2)
		}
		return pivotTestReply(1)(s)
	}})
	result, err := orm.Detach(t.Context(), db, orm.WithRelationWriteLimit(r, 1), writeParent{ID: 1}, writeTarget{ID: 10})
	if err == nil || result != nil {
		t.Fatal("oversized RETURNING accepted", result, err)
	}
}
func TestRelationWriteDeletionLifecycle(t *testing.T) {
	for _, force := range []bool{false, true} {
		var stages []string
		hooks := func() orm.Hooks[writePivot] {
			return orm.Hooks[writePivot]{
				ForceDeleting: func(context.Context, *database.Tx, writePivot) error {
					stages = append(stages, "force deleting")
					return nil
				},
				Deleting: func(context.Context, *database.Tx, writePivot) error { stages = append(stages, "deleting"); return nil },
				Deleted: func(context.Context, *database.Tx, orm.Changes[writePivot]) error {
					stages = append(stages, "deleted")
					return nil
				},
				ForceDeleted: func(context.Context, *database.Tx, orm.Changes[writePivot]) error {
					stages = append(stages, "force deleted")
					return nil
				},
			}
		}
		_, _, pivots, r := writeRelationTables()
		observer, err := orm.NewDeletionObserver[writePivot]("deletion").Declare(hooks)
		if err != nil {
			t.Fatal(err)
		}
		var statements []string
		db := hookDB(t, testConnector{statements: &statements, respond: pivotTestReply(1)}, observer)
		if force {
			_, err = orm.ForceDetach(t.Context(), db, r.WithTrashedPivot(), orm.SoftDeletes(pivots.DeletedAt), writeParent{ID: 1}, writeTarget{ID: 10})
		} else {
			_, err = orm.Detach(t.Context(), db, r, writeParent{ID: 1}, writeTarget{ID: 10})
		}
		if err != nil {
			t.Fatal(err)
		}
		want := "[deleting deleted]"
		if force {
			want = "[force deleting deleting deleted force deleted]"
		}
		if fmt.Sprint(stages) != want || len(statements) != 5 {
			t.Fatal(stages, statements)
		}
	}
}
