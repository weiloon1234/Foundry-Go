package pgsmoke_test

import "github.com/weiloon1234/Foundry-Go/model"

import (
	"context"
	"errors"
	"testing"

	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestStage5bRelationWritesPostgres(t *testing.T) {
	statements := 0
	db := postgres.Isolate(t).Open(t, database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
		if e.Operation == "query" {
			statements++
		}
	}))
	ctx := t.Context()
	for _, sql := range []string{
		`CREATE TABLE joined_parents(id bigint PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_targets(id bigint PRIMARY KEY,parent_id bigint NOT NULL,name text NOT NULL,rank bigint NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_pivots(id bigint PRIMARY KEY,parent_id bigint REFERENCES joined_parents(id),target_id bigint REFERENCES joined_targets(id),kind text NOT NULL,role text NOT NULL,deleted_at timestamptz)`,
		`INSERT INTO joined_parents VALUES(1,'parent',NULL)`,
		`INSERT INTO joined_targets VALUES(10,1,'target',1,NULL),(11,1,'second',2,NULL)`,
	} {
		_, err := db.Exec(ctx, sql)
		must(t, err)
	}
	parent, target := j.Parent{ID: 1}, j.Target{ID: 10}
	create := func(id int64) []orm.Assignment[j.Pivot] {
		return []orm.Assignment[j.Pivot]{orm.KeySet(j.Pivots.ID, id), orm.ColumnSet(j.Pivots.Kind, "parent"), orm.TextSet(j.Pivots.Role, "member")}
	}
	for _, id := range []int64{100, 101} {
		statements = 0
		row, err := orm.Attach(ctx, db, j.Peers, parent, target, create(id)...)
		must(t, err)
		if row.ID != id || statements != 4 {
			t.Fatal(row, statements)
		}
	}
	statements = 0
	updated, err := orm.UpdatePivot(ctx, db, j.Peers, parent, target, orm.TextSet(j.Pivots.Role, "admin"))
	must(t, err)
	if len(updated) != 2 || statements != 4 {
		t.Fatal(len(updated), statements)
	}
	persisted, err := orm.Count(ctx, db, j.Pivots.Query().Where(orm.TextEq(j.Pivots.Role, "admin")))
	must(t, err)
	if persisted != 2 {
		t.Fatal(persisted)
	}
	removed, err := orm.Detach(ctx, db, j.Peers, parent, target)
	must(t, err)
	if len(removed) != 2 {
		t.Fatal(removed)
	}
	n, err := orm.Count(ctx, db, j.Pivots.Query().OnlyTrashed())
	must(t, err)
	if n != 2 {
		t.Fatal(n)
	}
	purged, err := orm.ForceDetach(ctx, db, j.Peers.WithTrashedPivot(), j.Pivots.Soft, parent, target)
	must(t, err)
	if len(purged) != 2 {
		t.Fatal(purged)
	}
	changes, err := orm.Sync(ctx, db, j.Peers, parent, []j.Target{target}, create(102), nil)
	must(t, err)
	if len(changes.Attached) != 1 {
		t.Fatal(changes)
	}
	changes, err = orm.Toggle(ctx, db, j.Peers, parent, []j.Target{target}, create(103)...)
	must(t, err)
	if len(changes.Detached) != 1 {
		t.Fatal(changes)
	}
	changes, err = orm.Toggle(ctx, db, j.Peers, parent, []j.Target{target}, create(103)...)
	must(t, err)
	if len(changes.Attached) != 1 {
		t.Fatal(changes)
	}
	_, err = orm.Attach(ctx, db, j.Peers.WherePivot(orm.TextEq(j.Pivots.Role, "admin")), parent, target, create(104)...)
	if !errors.Is(err, fault.Invalid) {
		t.Fatal("filtered attachment", err)
	}
	missing, err := orm.Find(ctx, db, j.Pivots.Table, 104)
	must(t, err)
	if missing.IsSet() {
		t.Fatal("failed postcondition persisted")
	}
	// Caller-supplied endpoint defaults may be overridden, but single-attach postconditions must reject a different endpoint.
	defaults := append(create(104), orm.NullableNumberSet(j.Pivots.TargetID, 11))
	_, err = orm.Attach(ctx, db, j.Peers, parent, target, defaults...)
	if !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	_, err = orm.Attach(ctx, db, j.MorphPeers, parent, target, orm.KeySet(j.Pivots.ID, 105), orm.TextSet(j.Pivots.Role, "member"))
	must(t, err)
	morph, err := orm.RequireKey(ctx, db, j.Pivots.Table, 105)
	must(t, err)
	if morph.Kind != "parent" {
		t.Fatal(morph.Kind)
	}
	rollback := errors.New("outer rollback")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, e := orm.Attach(ctx, tx, j.Peers, parent, target, create(106)...)
		if e != nil {
			return e
		}
		_, e = orm.Attach(ctx, tx, j.Peers.WherePivot(orm.TextEq(j.Pivots.Role, "admin")), parent, target, create(107)...)
		if !errors.Is(e, fault.Invalid) {
			t.Fatalf("savepoint failure: %v", e)
		}
		if _, e = orm.RequireKey(ctx, tx, j.Pivots.Table, 106); e != nil {
			return e
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	missing, err = orm.Find(ctx, db, j.Pivots.Table, 106)
	must(t, err)
	if missing.IsSet() {
		t.Fatal("outer rollback persisted")
	}
}

func TestStage5bAttachEndpointHookRollbackPostgres(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	for _, sql := range []string{
		`CREATE TABLE joined_parents(id bigint PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_targets(id bigint PRIMARY KEY,parent_id bigint NOT NULL,name text NOT NULL,rank bigint NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_pivots(id bigint PRIMARY KEY,parent_id bigint REFERENCES joined_parents(id),target_id bigint REFERENCES joined_targets(id),kind text NOT NULL,role text NOT NULL,deleted_at timestamptz)`,
		`INSERT INTO joined_parents VALUES(1,'parent',NULL)`,
		`INSERT INTO joined_targets VALUES(10,1,'target',1,NULL)`,
	} {
		_, err := db.Exec(ctx, sql)
		must(t, err)
	}
	for _, endpoint := range []string{"source", "target"} {
		t.Run(endpoint, func(t *testing.T) {
			called, committed := false, false
			p := orm.Define("joined_pivots", func(b orm.Builder[j.Pivot], m *j.Pivot) j.PivotFields {
				table := orm.TableOf(b, &m.ID)
				return j.PivotFields{Table: table, ID: table.Key(), ParentID: orm.NullableNumberOf(b, &m.ParentID), TargetID: orm.NullableNumberOf(b, &m.TargetID), Kind: orm.ColumnOf(b, &m.Kind, orm.Codec(codec.String[model.MorphName]())), Role: orm.TextOf(b, &m.Role), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Parent: orm.OneOf(b, &m.Parent), Target: orm.OneOf(b, &m.Target), MorphParent: orm.OneOf(b, &m.MorphParent), MorphTarget: orm.OneOf(b, &m.MorphTarget)}
			}, orm.WithHooks(func() orm.Hooks[j.Pivot] {
				return orm.Hooks[j.Pivot]{
					Created: func(ctx context.Context, tx *database.Tx, _ orm.Changes[j.Pivot]) error {
						called = true
						if endpoint == "source" {
							_, err := orm.Delete(ctx, tx, j.Parents.Table.Query(), j.Parents.Table.Key(), 1)
							return err
						}
						_, err := orm.Delete(ctx, tx, j.Targets.Table.Query(), j.Targets.Table.Key(), 10)
						return err
					},
					AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[j.Pivot]) error { committed = true; return nil },
				}
			}))
			r := orm.ManyToManyOf(j.Parents.Peers, orm.KeyExpr(j.Parents.ID), orm.NullableNumberExpr(p.ParentID), orm.NullableNumberExpr(p.TargetID), orm.KeyExpr(j.Targets.ID))
			out, err := orm.Attach(ctx, db, r, j.Parent{ID: 1}, j.Target{ID: 10}, orm.KeySet(p.ID, 100), orm.ColumnSet(p.Kind, "parent"), orm.TextSet(p.Role, "member"))
			if !errors.Is(err, fault.Invalid) || out.ID != 0 || !called || committed {
				t.Fatal("endpoint hook accepted", err, called, committed)
			}
			_, err = orm.RequireKey(ctx, db, j.Parents.Table, 1)
			must(t, err)
			_, err = orm.RequireKey(ctx, db, j.Targets.Table, 10)
			must(t, err)
			candidate, err := orm.Find(ctx, db, j.Pivots.Table, 100)
			must(t, err)
			if candidate.IsSet() {
				t.Fatal("rejected attachment persisted")
			}
		})
	}
}
