package pgsmoke_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type joinNode struct {
	ID        int64
	ParentID  value.Nullable[int64]
	Note      value.Nullable[string]
	DeletedAt value.Nullable[temporal.DateTime]
}
type joinTable struct {
	orm.Table[joinNode, int64]
	ParentID  orm.NullableColumn[joinNode, int64]
	Note      orm.NullableText[joinNode, string]
	DeletedAt orm.SoftDelete[joinNode, temporal.DateTime]
}
type joinLeft struct{}
type joinRight struct{}
type joinThird struct{}
type joinRow struct {
	LeftID, RightID value.Nullable[int64]
	Note            value.Nullable[string]
}
type joinRowFields struct {
	rows.Row[joinRow]
	LeftID, RightID rows.Field[joinRow, value.Nullable[int64]]
	Note            rows.Field[joinRow, value.Nullable[string]]
}

func TestStage6b1Joins(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE join_nodes (id bigint PRIMARY KEY,parent_id bigint,note text,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO join_nodes VALUES (1,NULL,'root',NULL),(2,1,NULL,NULL),(3,99,NULL,NULL),(4,1,'deleted',now()),(5,NULL,NULL,NULL)`)
	must(t, err)
	table := orm.Define("join_nodes", func(b orm.Builder[joinNode], r *joinNode) joinTable {
		return joinTable{orm.TableOf(b, &r.ID), orm.NullableColumnOf(b, &r.ParentID), orm.NullableTextOf(b, &r.Note), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	schema := rows.Define(func(b rows.Builder[joinRow], r *joinRow) joinRowFields {
		return joinRowFields{b.Row(), rows.NullableNumberOf(b, &r.LeftID), rows.NullableNumberOf(b, &r.RightID), rows.NullableTextOf(b, &r.Note)}
	})
	must(t, orm.Check(table))
	must(t, rows.Check(schema))
	a := rows.As[joinLeft](table.Query().Where(table.Key().Ne(5)), "a")
	b := rows.As[joinRight](table.Query().Where(table.Key().Ne(2), table.Key().Ne(3)), "b")
	on := rows.On(expr.At(rows.Scope(a), table.ParentID), expr.At(rows.Scope(b), table.Key()))
	inner := rows.InnerJoin(a, b, on)
	got, err := rows.Select(inner, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(expr.At(rows.LeftScope(inner, rows.Scope(a)), table.Key())).Value()), rows.MapSelected(schema.RightID, expr.NullableRow(expr.At(rows.RightScope(inner, rows.Scope(b)), table.Key())).Value()), rows.Map(schema.Note, expr.At(rows.RightScope(inner, rows.Scope(b)), table.Note))).All(ctx, db)
	must(t, err)
	assertJoinRows(t, got, 1, 0, 0)
	left := rows.LeftJoin(a, b, on)
	ls, rs := rows.LeftScope(left, rows.Scope(a)), rows.NullableRightScope(left, rows.Scope(b))
	leftQuery := rows.Select(left, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(expr.At(ls, table.Key())).Value()), rows.Map(schema.RightID, expr.AtNullable(rs, table.Key())), rows.Map(schema.Note, expr.AtNullableValue(rs, table.Note)))
	got, err = leftQuery.All(ctx, db)
	must(t, err)
	assertJoinRows(t, got, 3, 0, 2)
	missing, err := leftQuery.Where(expr.AtNullable(rs, table.Key()).IsNull()).All(ctx, db)
	must(t, err)
	assertJoinRows(t, missing, 2, 0, 2)
	right := rows.RightJoin(a, b, on)
	rl, rr := rows.NullableLeftScope(right, rows.Scope(a)), rows.RightScope(right, rows.Scope(b))
	got, err = rows.Select(right, schema, rows.Map(schema.LeftID, expr.AtNullable(rl, table.Key())), rows.MapSelected(schema.RightID, expr.NullableRow(expr.At(rr, table.Key())).Value()), rows.Map(schema.Note, expr.At(rr, table.Note))).All(ctx, db)
	must(t, err)
	assertJoinRows(t, got, 2, 1, 0)
	full := rows.FullJoin(a, b, on)
	fl, fr := rows.NullableLeftScope(full, rows.Scope(a)), rows.NullableRightScope(full, rows.Scope(b))
	got, err = rows.Select(full, schema, rows.Map(schema.LeftID, expr.AtNullable(fl, table.Key())), rows.Map(schema.RightID, expr.AtNullable(fr, table.Key())), rows.Map(schema.Note, expr.AtNullableValue(fr, table.Note))).All(ctx, db)
	must(t, err)
	assertJoinRows(t, got, 4, 1, 2)
	cross := rows.CrossJoin(a, b)
	cl, cr := rows.LeftScope(cross, rows.Scope(a)), rows.RightScope(cross, rows.Scope(b))
	got, err = rows.Select(cross, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(expr.At(cl, table.Key())).Value()), rows.MapSelected(schema.RightID, expr.NullableRow(expr.At(cr, table.Key())).Value()), rows.Map(schema.Note, expr.At(cr, table.Note))).All(ctx, db)
	must(t, err)
	assertJoinRows(t, got, 6, 0, 0)
	c := rows.As[joinThird](table.Query().Where(table.Key().Eq(1)), "c")
	chain := rows.CrossJoin(left, c)
	chainL, chainR := rows.LeftScope(chain, ls), rows.LeftNullableScope(chain, rs)
	got, err = rows.Select(chain, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(expr.At(chainL, table.Key())).Value()), rows.Map(schema.RightID, expr.AtNullable(chainR, table.Key())), rows.Map(schema.Note, expr.AtNullableValue(chainR, table.Note))).All(ctx, db)
	must(t, err)
	assertJoinRows(t, got, 3, 0, 2)
}
func assertJoinRows(t *testing.T, got []joinRow, count, nullLeft, nullRight int) {
	t.Helper()
	l, r := 0, 0
	for _, v := range got {
		if v.LeftID.IsNull() {
			l++
		}
		if v.RightID.IsNull() {
			r++
		}
		if v.LeftID == value.Of(int64(4)) || v.RightID == value.Of(int64(4)) {
			t.Fatal("soft-deleted join row", got)
		}
	}
	if len(got) != count || l != nullLeft || r != nullRight {
		t.Fatalf("join rows: %+v; want count=%d nullLeft=%d nullRight=%d", got, count, nullLeft, nullRight)
	}
}
