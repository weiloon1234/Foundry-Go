package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Lane B compiles/vets this source; the lead runs a test-owned isolated namespace.
func TestConflictPersistsDefaultsAndRollsBackAtomicFailures(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE hook_members(id bigint PRIMARY KEY,name text NOT NULL DEFAULT 'database',note text,body bytea NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	calls := 0
	table := declareHookMembers(func() orm.Hooks[h.Member] { calls++; return orm.Hooks[h.Member]{} })
	incoming := orm.OnConflict(orm.ConflictKey(table.Key())).DoUpdate(orm.ConflictIncoming(table.Name), orm.ConflictIncoming(table.Note))
	got, err := orm.Upsert(ctx, db, table.Query(), incoming, orm.Set(table.Key(), 1), orm.Set(table.Name, " original "), orm.Set(table.Body, []byte{1}))
	must(t, err)
	first, ok := got.Get()
	if !ok || first.Name != "original" {
		t.Fatal(first)
	}
	got, err = orm.Upsert(ctx, db, table.Query(), incoming, orm.Set(table.Key(), 1), orm.Set(table.Body, []byte{2}))
	must(t, err)
	stored, ok := got.Get()
	if !ok || stored.Name != "database" || stored.Note.IsNull() == false || !stored.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal(stored)
	}
	skipped, err := orm.Upsert(ctx, db, table.Query(), orm.OnConflictConstraint[h.Member]("hook_members_pkey").DoNothing(), orm.Set(table.Key(), 1), orm.Set(table.Body, []byte{3}))
	must(t, err)
	if skipped.IsSet() {
		t.Fatal("DO NOTHING returned a model")
	}
	skipped, err = orm.Upsert(ctx, db, table.Query(), incoming.Where(orm.Eq(table.Name, "absent")), orm.Set(table.Key(), 1), orm.Set(table.Name, "changed"), orm.Set(table.Body, []byte{3}))
	must(t, err)
	if skipped.IsSet() {
		t.Fatal("false update condition returned a model")
	}
	// PostgreSQL rejects repeated conflict targets in the same DO UPDATE statement.
	_, err = orm.UpsertMany(ctx, db, table.Query(), incoming, [][]orm.Assignment[h.Member]{
		{orm.Set(table.Key(), 2), orm.Set(table.Name, "new"), orm.Set(table.Body, []byte{2})},
		{orm.Set(table.Key(), 1), orm.Set(table.Name, "changed"), orm.Set(table.Body, []byte{3})},
		{orm.Set(table.Key(), 1), orm.Set(table.Name, "again"), orm.Set(table.Body, []byte{4})},
	})
	if err == nil {
		t.Fatal("repeated target accepted")
	}
	absent, err := orm.Find(ctx, db, table.Table, 2)
	must(t, err)
	if absent.IsSet() {
		t.Fatal("batch partially persisted")
	}
	veto := errors.New("rollback enclosing transaction")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := orm.Upsert(ctx, tx, table.Query(), incoming, orm.Set(table.Key(), 1), orm.Set(table.Name, "rolled back"), orm.Set(table.Body, []byte{5}))
		if err != nil {
			return err
		}
		return veto
	})
	if !errors.Is(err, veto) {
		t.Fatal(err)
	}
	persisted, err := orm.RequireKey(ctx, db, table.Table, 1)
	must(t, err)
	if persisted.Name != "database" || calls != 0 {
		t.Fatal("rollback or lifecycle", persisted.Name, calls)
	}
}

type conflictTenant struct {
	ID     int64
	Tenant int64
	Name   string
}
type conflictTenantTable struct {
	orm.Table[conflictTenant, int64]
	Tenant orm.Number[conflictTenant, int64]
	Name   orm.Text[conflictTenant, string]
}
type conflictTenantContext struct{}

func TestConflictTenantSuppressionRollsBackOtherBatchRows(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := context.WithValue(t.Context(), conflictTenantContext{}, int64(7))
	table := orm.Define("conflict_tenants", func(b orm.Builder[conflictTenant], m *conflictTenant) conflictTenantTable {
		return conflictTenantTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Tenant), orm.TextOf(b, &m.Name)}
	}, orm.WithGlobalScopes(func(t conflictTenantTable) []orm.GlobalScope[conflictTenant] {
		return []orm.GlobalScope[conflictTenant]{orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[conflictTenant], error) {
			id, ok := ctx.Value(conflictTenantContext{}).(int64)
			if !ok {
				return orm.Predicate[conflictTenant]{}, errors.New("tenant missing")
			}
			return orm.Eq(t.Tenant, id), nil
		})}
	}))
	_, err := db.Exec(ctx, `CREATE TABLE conflict_tenants(id bigint PRIMARY KEY,tenant bigint NOT NULL,name text NOT NULL)`)
	must(t, err)
	_, err = orm.Insert(ctx, db, table.Table, orm.Set(table.Key(), 1), orm.Set(table.Tenant, 9), orm.Set(table.Name, "private"))
	must(t, err)
	policy := orm.OnConflict(orm.ConflictKey(table.Key())).DoUpdate(orm.ConflictIncoming(table.Name))
	_, err = orm.UpsertMany(ctx, db, table.Query(), policy, [][]orm.Assignment[conflictTenant]{
		{orm.Set(table.Key(), 2), orm.Set(table.Tenant, 7), orm.Set(table.Name, "new")},
		{orm.Set(table.Key(), 1), orm.Set(table.Tenant, 7), orm.Set(table.Name, "forbidden")},
	})
	if !errors.Is(err, fault.Conflict) {
		t.Fatal("tenant violation", err)
	}
	rows, err := orm.All(ctx, db, orm.WithoutGlobalScopes(table.Query()))
	must(t, err)
	if len(rows) != 1 || rows[0].Name != "private" {
		t.Fatal("scope failure partially committed", rows)
	}
	_, err = orm.Upsert(t.Context(), db, table.Query(), policy, orm.Set(table.Key(), 3), orm.Set(table.Tenant, 7), orm.Set(table.Name, "missing context"))
	if err == nil {
		t.Fatal("missing tenant accepted")
	}
	rows, err = orm.All(ctx, db, orm.WithoutGlobalScopes(table.Query()))
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("failed scope wrote rows")
	}
}
