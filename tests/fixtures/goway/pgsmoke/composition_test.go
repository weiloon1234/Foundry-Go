package pgsmoke_test

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type treeNode struct {
	ID       int64
	ParentID int64
}
type treeTable struct {
	orm.Table[treeNode, int64]
	ParentID orm.Number[treeNode, int64]
}

func TestStage6c2CTEsRecursionAndSets(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE composition_nodes (id bigint PRIMARY KEY,parent_id bigint NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO composition_nodes VALUES (1,0),(2,1),(3,1)`)
	must(t, err)
	table := orm.Define("composition_nodes", func(b orm.Builder[treeNode], n *treeNode) treeTable {
		return treeTable{orm.TableOf(b, &n.ID), orm.NumberOf(b, &n.ParentID)}
	})
	anchor := rows.ModelRecord(table.Query().Where(orm.KeyEq(table.Key(), 1)))
	step := func(self rows.RecursiveSelf[treeNode]) rows.RecordSource[treeNode] {
		p, c := rows.AsRecord[joinLeft](self, "parent"), rows.As[joinRight](table.Query(), "child")
		j := rows.InnerJoin(c, p, rows.On(expr.At(rows.Scope(c), orm.NumberExpr(table.ParentID)), expr.At(rows.Scope(p), orm.KeyExpr(table.Key()))))
		return rows.SelectRecord(j, rows.LeftScope(j, rows.Scope(c)))
	}
	for _, c := range []rows.CommonTable[treeNode]{rows.RecursiveCTE("tree", anchor, step), rows.RecursiveAllCTE("tree", anchor, step)} {
		a := rows.AsRecord[joinLeft](c, "result")
		got, err := rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a)).OrderBy(expr.At(rows.Scope(a), orm.KeyExpr(table.Key())).Asc())).All(ctx, db)
		must(t, err)
		if !reflect.DeepEqual(got, []treeNode{{1, 0}, {2, 1}, {3, 1}}) {
			t.Fatal(got)
		}
	}
	left, right := rows.ModelRecord(table.Query().Where(orm.KeyIn(table.Key(), 1, 2))), rows.ModelRecord(table.Query().Where(orm.KeyIn(table.Key(), 2, 3)))
	sets := []rows.SetQuery[treeNode]{rows.Union(left, right), rows.UnionAll(left, right), rows.Intersect(left, right), rows.IntersectAll(left, right), rows.Except(left, right), rows.ExceptAll(left, right)}
	wants := [][]int64{{1, 2, 3}, {1, 2, 2, 3}, {2}, {2}, {1}, {1}}
	for i, q := range sets {
		got, err := rows.ReadSet(q.OrderBy(expr.At(q.Scope(), orm.KeyExpr(table.Key())).Asc())).All(ctx, db)
		must(t, err)
		var ids []int64
		for _, r := range got {
			ids = append(ids, r.ID)
		}
		if !reflect.DeepEqual(ids, wants[i]) {
			t.Fatal(i, ids)
		}
	}
	vals := rows.Values(rows.SelectScalar(rows.From(table.Query()), expr.Row(expr.Value(orm.NumberExpr(table.ParentID))).Value()))
	combined := rows.UnionValues(vals, vals)
	got, err := rows.ReadValues(combined.OrderBy(combined.Value().Asc())).All(ctx, db)
	must(t, err)
	if !reflect.DeepEqual(got, []int64{0, 1}) {
		t.Fatal(got)
	}
	// Dependencies retain one shared definition and the retained inner lock.
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		c := rows.TransactionCTE("claim", rows.LockedRecord(orm.Lock(table.Query().Where(orm.KeyEq(table.Key(), 1))))).Materialized()
		a := rows.AsTransaction[joinLeft](c, "a")
		got, err := rows.SelectTransactionRecord(a, rows.TransactionScope(a)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(got) != 1 {
			t.Fatal(got)
		}
		return db.Transaction(ctx, func(other *database.Tx) error {
			c := rows.TransactionCTE("claim", rows.LockedRecord(orm.Lock(table.Query().Where(orm.KeyEq(table.Key(), 1))).SkipLocked()))
			a := rows.AsTransaction[joinRight](c, "b")
			got, err := rows.SelectTransactionRecord(a, rows.TransactionScope(a)).All(ctx, other)
			if err == nil && len(got) != 0 {
				t.Fatal("CTE lost lock", got)
			}
			return err
		})
	}))
}
