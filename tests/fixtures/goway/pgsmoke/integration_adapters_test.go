package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type adapterRecord struct {
	ID     int64
	Tenant string
}
type adapterTable struct {
	orm.Table[adapterRecord, int64]
	Tenant orm.Text[adapterRecord, string]
}

func TestDeclarationIdentityBindingAndFactory(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE adapter_records (id bigint PRIMARY KEY,tenant text NOT NULL)`)
	must(t, err)
	records := orm.Define("adapter_records", func(b orm.Builder[adapterRecord], m *adapterRecord) adapterTable {
		return adapterTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Tenant)}
	})
	f, err := factory.Define(records.Table, func(_ context.Context, n factory.Sequence) ([]orm.Assignment[adapterRecord], error) {
		return []orm.Assignment[adapterRecord]{orm.KeySet(records.Key(), int64(n)), orm.TextSet(records.Tenant, "allowed")}, nil
	})
	must(t, err)
	created, err := f.CreateMany(ctx, db, 3)
	must(t, err)
	if len(created) != 3 {
		t.Fatal("batch cardinality")
	}
	identity := orm.IdentityOf(records.Table, records.Key())
	keys, err := identity.ActiveKeys(ctx, db, []int64{1, 2, 3})
	must(t, err)
	if len(keys) != 3 {
		t.Fatal("identity cardinality")
	}
	resolver := modelbinding.ByKey(db, orm.Keyed(records.Query().Where(orm.TextEq(records.Tenant, "allowed")), records.Key()), func(id int64) int64 { return id })
	found, err := resolver.Resolve(ctx, 2)
	must(t, err)
	if found.ID != 2 {
		t.Fatal("key lookup")
	}
	failing, err := f.AfterCreating(func(context.Context, database.Transactor, adapterRecord) (adapterRecord, error) {
		return adapterRecord{}, errors.New("rollback")
	})
	must(t, err)
	if _, err := failing.CreateMany(ctx, db, 2); err == nil {
		t.Fatal("failed hook committed")
	}
	count, err := orm.Count(ctx, db, records.Query())
	must(t, err)
	if count != 3 {
		t.Fatal("partial batch persisted", count)
	}
}
