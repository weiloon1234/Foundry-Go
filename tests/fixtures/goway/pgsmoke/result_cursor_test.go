package pgsmoke_test

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestStage6c3ResultCursorAndReadTerminals(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE result_cursor_nodes (id bigint PRIMARY KEY,parent_id bigint NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO result_cursor_nodes VALUES (1,0),(2,0),(3,1)`)
	must(t, err)
	table := orm.Define("result_cursor_nodes", func(b orm.Builder[treeNode], r *treeNode) treeTable {
		return treeTable{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.ParentID)}
	})
	c := rows.CursorFor(rows.ModelRecord(table.Query()))
	c = c.OrderBy(expr.At(c.Scope(), table.ParentID).Asc()).UniqueBy(expr.Group(expr.At(c.Scope(), table.Key())))
	first, err := c.Paginate(ctx, db, orm.CursorRequest[treeNode]{Size: 1})
	must(t, err)
	next, err := c.Paginate(ctx, db, orm.CursorRequest[treeNode]{Size: 1, After: first.Next})
	must(t, err)
	previous, err := c.Paginate(ctx, db, orm.CursorRequest[treeNode]{Size: 1, Before: next.Previous})
	must(t, err)
	if first.Items[0].ID != 1 || next.Items[0].ID != 2 || !reflect.DeepEqual(previous.Items, first.Items) {
		t.Fatal(first, next, previous)
	}
	scalar := rows.Read(rows.SelectValue(table.Query(), table.ParentID)).OrderBy(expr.Value(table.Key()).Asc())
	page, err := scalar.Paginate(ctx, db, orm.PageRequest{Number: 2, Size: 2})
	must(t, err)
	if page.Total != 3 || !reflect.DeepEqual(page.Items, []int64{1}) {
		t.Fatal(page)
	}
	nullable := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(table.Query()), expr.Nullable(expr.Row(expr.Value(table.Key())).Value()))))
	nullable = nullable.UniqueBy(expr.Group(nullable.Value()))
	p, err := nullable.Paginate(ctx, db, orm.CursorRequest[value.Nullable[int64]]{Size: 2})
	must(t, err)
	if len(p.Items) != 2 || !p.Next.IsSet() {
		t.Fatal(p)
	}
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		a := rows.AsTransaction[joinLeft](rows.TransactionOf(rows.ModelRecord(table.Query())), "nodes")
		q := rows.SelectTransactionValue(a, expr.Row(expr.At(rows.TransactionScope(a), table.Key())).Value())
		got, err := rows.ForUpdate(q).Of(rows.Target(rows.TransactionScope(a))).OrderBy(expr.At(rows.TransactionScope(a), table.Key()).Asc()).Limit(1).All(ctx, tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, []int64{1}) {
			t.Fatal(got)
		}
		return nil
	}))
}
