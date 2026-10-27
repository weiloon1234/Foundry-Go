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

func TestStage6b4TransactionComposition(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE transaction_composition_nodes (id bigint PRIMARY KEY,parent_id bigint NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO transaction_composition_nodes VALUES (1,0),(2,1),(3,1),(4,0)`)
	must(t, err)
	table := orm.Define("transaction_composition_nodes", func(b orm.Builder[treeNode], r *treeNode) treeTable {
		return treeTable{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.ParentID)}
	})
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		a := rows.AsTransaction[joinLeft](rows.LockedRecord(table.Query().Where(table.ParentID.Eq(0)).ForUpdate()), "parent")
		b := rows.AsTransaction[joinRight](rows.TransactionOf(rows.ModelRecord(table.Query())), "child")
		c := rows.TransactionCorrelate(a, b)
		os, is := rows.TransactionOuterScope(c, rows.TransactionScope(a)), rows.TransactionInnerScope(c, rows.TransactionScope(b))
		c = c.Where(expr.Equal(expr.At(is, table.ParentID), expr.At(os, table.Key())))
		record := rows.SelectTransactionCorrelatedRecord(c, is).OrderBy(expr.At(is, table.Key()).Desc()).Limit(1).ForUpdate().Of(rows.Target(is)).SkipLocked()
		lat := rows.AsTransactionLateral[joinThird](record, "latest")
		joined := rows.TransactionLeftJoinLateral(a, lat)
		selected := rows.SelectTransactionValue(joined, expr.Row(expr.AtNullable(rows.NullableRightScope(joined, rows.TransactionLateralScope(lat)), table.Key())).Value()).OrderBy(expr.At(rows.LeftScope(joined, rows.TransactionScope(a)), table.Key()).Asc())
		got, err := selected.All(ctx, tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(3)), value.Null[int64]()}) {
			t.Fatal(got)
		}
		scalar := rows.SelectTransactionCorrelatedScalar(c, expr.Row(expr.At(is, table.Key())).Value()).OrderBy(expr.At(is, table.Key()).Asc()).Limit(1).ForShare()
		got, err = rows.SelectTransactionValue(a, rows.TransactionCorrelatedScalarRowQuery(scalar).Value()).Where(c.Exists()).All(ctx, tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(2))}) {
			t.Fatal(got)
		}
		ids := rows.SelectTransactionValue(b, expr.Row(expr.At(rows.TransactionScope(b), table.ParentID)).Value()).Where(expr.Greater(expr.At(rows.TransactionScope(b), table.ParentID), expr.Row(expr.At(rows.TransactionScope(b), table.ParentID)).Value().Param(0)))
		independent := rows.TransactionScalarOf(ids).ForShare()
		parents, err := rows.SelectTransactionRecord(a, rows.TransactionScope(a)).Where(rows.TransactionInQuery(expr.At(rows.TransactionScope(a), table.Key()), independent), rows.TransactionExistsQuery(a, independent)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(parents) != 1 || parents[0].ID != 1 {
			t.Fatal(parents)
		}
		return nil
	}))
	// Reads and nested locks must leave persisted data intact after the caller commits.
	n, err := table.Query().Count(ctx, db)
	must(t, err)
	if n != 4 {
		t.Fatal(n)
	}
}
