package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Lane B compiles/vets this source; the lead runs PostgreSQL.
func TestRetrievalClosedTransactionAndTenantIsolation(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE retrieval_members(id bigint PRIMARY KEY,tenant_id bigint NOT NULL,name text NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO retrieval_members VALUES(1,7,'one'),(2,8,'two')`)
	must(t, err)
	type member struct {
		ID       int64
		TenantID int64
		Name     string
	}
	type fields struct {
		orm.Table[member, int64]
		ID       orm.Key[member, int64]
		TenantID orm.Number[member, int64]
		Name     orm.Text[member, string]
	}
	factories, calls := 0, 0
	table := orm.Define("retrieval_members", func(b orm.Builder[member], m *member) fields {
		t := orm.TableOf(b, &m.ID)
		return fields{t, t.Key(), orm.NumberOf(b, &m.TenantID), orm.TextOf(b, &m.Name)}
	}, orm.WithGlobalScopes(func(t fields) []orm.GlobalScope[member] {
		return []orm.GlobalScope[member]{orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[member], error) {
			id, err := coremodels.TenantID(ctx)
			return orm.NumberEq(t.TenantID, id), err
		})}
	}), orm.WithRetrievalHooks(func() orm.RetrievalHooks[member] {
		factories++
		return orm.RetrievalHooks[member]{Retrieved: func(ctx context.Context, ex database.Executor, m member) error {
			// Re-enter the same executor, including a transaction: rows must be closed.
			var n int
			if err := database.ScanOne(ctx, ex, "SELECT 1", nil, &n); err != nil {
				return err
			}
			if n != 1 {
				return errors.New("nested read failed")
			}
			calls++
			return nil
		}}
	}))
	tenant := coremodels.TenantContext(ctx, 7)
	must(t, db.Transaction(tenant, func(tx *database.Tx) error {
		items, err := orm.All(tenant, tx, table.Query())
		if err == nil && (len(items) != 1 || items[0].TenantID != 7) {
			return errors.New("tenant read escaped")
		}
		return err
	}))
	if calls != 1 || factories != 1 {
		t.Fatal("read factories", calls, factories)
	}
	if _, err := orm.All(ctx, db, table.Query()); err == nil {
		t.Fatal("missing tenant opened read")
	}
	_, err = orm.Update(tenant, db, table.Table.Query(), table.Table.Key(), 2, orm.TextSet(table.Name, "forbidden"))
	if !errors.Is(err, database.NotFound) {
		t.Fatal("tenant write escaped", err)
	}
	got, err := orm.Raw(ctx, db, table.Table, `SELECT name,id,tenant_id FROM retrieval_members ORDER BY id`)
	must(t, err)
	if len(got) != 2 || got[1].Name != "two" || calls != 3 || factories != 2 {
		t.Fatal("raw retrieval or tenant write", got, calls, factories)
	}
	must(t, orm.Each(tenant, db, table.Query(), func(member) error { return nil }))
	if calls != 4 || factories != 3 {
		t.Fatal("stream retrieval", calls, factories)
	}
	count, err := orm.Count(tenant, db, table.Query())
	must(t, err)
	if count != 1 || factories != 3 {
		t.Fatal("scalar retrieved", count, factories)
	}
}
