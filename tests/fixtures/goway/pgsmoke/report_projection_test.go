package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestStage7ReportProjectionReads(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE report_projection_nodes (id bigint PRIMARY KEY,parent_id bigint NOT NULL)`)
	must(t, err)
	table := orm.Define("report_projection_nodes", func(b orm.Builder[treeNode], r *treeNode) treeTable {
		return treeTable{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.ParentID)}
	}, orm.WithGlobalScopes(func(d treeTable) []orm.GlobalScope[treeNode] {
		return []orm.GlobalScope[treeNode]{orm.Scope(d.Table, "visible", func(context.Context) (orm.Predicate[treeNode], error) { return d.ParentID.Gt(0), nil })}
	}))
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO report_projection_nodes VALUES (1,1),(2,1),(3,2),(4,0)`); err != nil {
			return err
		}
		q := rows.ReorderUnwindowed(rows.SelectValue(table.Query(), table.ParentID), expr.Value(table.Key()).Asc())
		count, err := rows.Count(ctx, tx, q)
		if err != nil {
			return err
		}
		if count != 3 {
			t.Fatal(count)
		}
		page, err := rows.Paginate(ctx, tx, q, orm.Page(2, 2))
		if err != nil {
			return err
		}
		if page.Total != 3 || len(page.Items) != 1 || page.Items[0] != 2 {
			t.Fatal(page)
		}
		simple, err := rows.SimplePaginate(ctx, tx, q, orm.Page(1, 2))
		if err != nil {
			return err
		}
		if !simple.HasMore || len(simple.Items) != 2 {
			t.Fatal(simple)
		}
		grouped := rows.Having(rows.GroupBy(rows.SelectValue(table.Query(), table.ParentID), expr.Group(expr.Value(table.ParentID))), expr.Count[treeNode]().Gt(1))
		count, err = rows.Count(ctx, tx, grouped)
		if err != nil {
			return err
		}
		if count != 1 {
			t.Fatal(count)
		}
		stop := errors.New("stop export")
		calls := 0
		err = rows.Each(ctx, tx, q, func(int64) error { calls++; return stop })
		if !errors.Is(err, stop) || calls != 1 {
			t.Fatal(calls, err)
		}
		_, err = rows.Count(ctx, tx, q) // Early stream close releases this transaction for another read.
		return err
	}))
}
