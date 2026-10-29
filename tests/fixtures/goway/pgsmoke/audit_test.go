package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Lane B compiles/vets this source; the lead runs it against PostgreSQL.
func TestAuditRowsShareRollbackAndRestoreTypedHistory(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE hook_members(id bigint PRIMARY KEY,name text NOT NULL DEFAULT 'database',note text,body bytea NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		for _, migration := range audit.Migrations() {
			for _, sql := range migration.SQL {
				if _, err := tx.Exec(ctx, sql); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	recorder, err := audit.New(audit.DefaultConfig())
	must(t, err)
	veto := errors.New("rollback after audit")
	var table h.MemberTable
	var captured record.Model[h.Member, int64]
	table = h.Declare(func() orm.Hooks[h.Member] {
		write := func(ctx context.Context, tx *database.Tx, c orm.Changes[h.Member]) error {
			history, err := orm.Audit(table.Table, c, orm.AuditPolicy[h.Member]{})
			if err != nil {
				return err
			}
			if err = recorder.RecordModel(ctx, tx, history.Entry()); err != nil {
				return err
			}
			m, _ := c.After().Get()
			if m.Name == "reject" {
				return veto
			}
			captured = history
			return nil
		}
		return orm.Hooks[h.Member]{Created: write, Updated: write, Deleted: write, Restored: write}
	})
	_, err = orm.Insert(ctx, db, table.Table, orm.KeySet(table.Key(), 1), orm.InputTextSet(table.Name, " stored "), orm.ColumnSet(table.Body, []byte{1}))
	must(t, err)
	// The rejected model and its already inserted audit row must both disappear.
	_, err = orm.Insert(ctx, db, table.Table, orm.KeySet(table.Key(), 2), orm.InputTextSet(table.Name, " reject "), orm.ColumnSet(table.Body, []byte{2}))
	if !errors.Is(err, veto) {
		t.Fatal(err)
	}
	count, err := orm.Count(ctx, db, table.Query())
	must(t, err)
	if count != 1 {
		t.Fatal("business rollback", count)
	}
	rows, err := db.Query(ctx, `SELECT count(*) FROM foundry_audit`)
	must(t, err)
	if !rows.Next() {
		t.Fatal("missing audit count")
	}
	must(t, rows.Scan(&count))
	must(t, rows.Close())
	if count != 1 {
		t.Fatal("audit row escaped rollback", count)
	}
	_, err = orm.Update(ctx, db, table.Table.Query(), table.Table.Key(), 1, orm.InputTextSet(table.Name, " updated "))
	must(t, err)
	subject, err := captured.Subject()
	must(t, err)
	history, err := audit.ModelHistory(ctx, db, recorder, subject, orm.PageRequest{Number: 1, Size: 10})
	must(t, err)
	if len(history.Items) != 2 {
		t.Fatal("stored history", len(history.Items))
	}
	field, err := orm.AuditRead(history.Items[0].Changes(), orm.InputTextExpr(table.Name))
	must(t, err)
	name, ok := field.Get()
	if !ok {
		t.Fatal("missing stored field")
	}
	stored, err := name.After().Get()
	must(t, err)
	v, _ := stored.Get()
	if v != "updated" {
		t.Fatal("historical codec", v)
	}
	body, err := orm.AuditRead(history.Items[0].Changes(), orm.ColumnExpr(table.Body))
	must(t, err)
	if body.IsSet() {
		t.Fatal("untouched body retained")
	}
}
