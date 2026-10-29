package pgsmoke_test

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"testing"
)

type recordItem struct {
	ID   int64
	Name string
}
type recordTable struct {
	orm.Table[recordItem, int64]
	Name orm.Text[recordItem, string]
}
type recordSummary struct{ Name string }
type recordFields struct {
	rows.Row[recordSummary]
	Name rows.Field[recordSummary, string]
}

func TestStage6c1CompleteRecords(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE record_items (id bigint PRIMARY KEY,name text NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO record_items VALUES (1,'first'),(2,'second')`)
	must(t, err)
	table := orm.Define("record_items", func(b orm.Builder[recordItem], r *recordItem) recordTable {
		return recordTable{orm.TableOf(b, &r.ID), orm.TextOf(b, &r.Name)}
	})
	schema := rows.Define(func(b rows.Builder[recordSummary], r *recordSummary) recordFields {
		return recordFields{b.Row(), rows.TextOf(b, &r.Name)}
	})
	a := rows.AsRecord[joinLeft](rows.ModelRecord(table.Query().OrderBy(orm.KeyAsc(table.Key())).Limit(1)), "item")
	got, err := rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a))).All(ctx, db)
	must(t, err)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatal(got)
	}
	p := rows.Select(rows.From(table.Query().Where(orm.KeyEq(table.Key(), 2))), schema, rows.Map(schema.Name, expr.Value(orm.TextExpr(table.Name))))
	derived := rows.AsRecord[joinRight](rows.RecordOf(p), "summary")
	summaries, err := rows.ReadRecord(rows.SelectRecord(derived, rows.Scope(derived))).All(ctx, db)
	must(t, err)
	if len(summaries) != 1 || summaries[0].Name != "second" {
		t.Fatal(summaries)
	}
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		locked := rows.AsTransaction[joinLeft](rows.LockedRecord(orm.Lock(table.Query().Where(orm.KeyEq(table.Key(), 1)))), "claimed")
		records, err := rows.SelectTransactionRecord(locked, rows.TransactionScope(locked)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(records) != 1 {
			t.Fatal(records)
		}
		// A separate transaction cannot acquire the retained inner lock.
		return db.Transaction(ctx, func(other *database.Tx) error {
			next := rows.AsTransaction[joinRight](rows.LockedRecord(orm.Lock(table.Query().Where(orm.KeyEq(table.Key(), 1))).SkipLocked()), "next")
			remaining, err := rows.SelectTransactionRecord(next, rows.TransactionScope(next)).All(ctx, other)
			if err == nil && len(remaining) != 0 {
				t.Fatal("lock escaped derived source", remaining)
			}
			return err
		})
	}))
}
