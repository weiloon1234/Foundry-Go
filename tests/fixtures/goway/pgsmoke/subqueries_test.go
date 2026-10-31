package pgsmoke_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type subqueryRow struct {
	ID      int64
	Missing value.Nullable[int64]
	Note    value.Nullable[string]
}
type subqueryFields struct {
	rows.Row[subqueryRow]
	ID      rows.Field[subqueryRow, int64]
	Missing rows.Field[subqueryRow, value.Nullable[int64]]
	Note    rows.Field[subqueryRow, value.Nullable[string]]
}

func TestStage6b2SubqueriesAndOnFilters(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE subquery_nodes (id bigint PRIMARY KEY,parent_id bigint,note text,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO subquery_nodes VALUES (1,NULL,'root',NULL),(2,1,NULL,NULL),(3,99,'other',NULL),(4,1,'deleted',now()),(5,NULL,NULL,NULL)`)
	must(t, err)
	table := orm.Define("subquery_nodes", func(b orm.Builder[joinNode], r *joinNode) joinTable {
		return joinTable{orm.TableOf(b, &r.ID), orm.NullableColumnOf(b, &r.ParentID), orm.NullableTextOf(b, &r.Note), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	fields := rows.Define(func(b rows.Builder[subqueryRow], r *subqueryRow) subqueryFields {
		return subqueryFields{b.Row(), rows.NumberOf(b, &r.ID), rows.NullableNumberOf(b, &r.Missing), rows.NullableTextOf(b, &r.Note)}
	})
	source := rows.From(table.Query())
	ids := expr.Value(orm.KeyExpr(table.Key()))
	missing := rows.SelectScalar(rows.From(table.Query().Where(orm.KeyEq(table.Key(), 999))), expr.AsSelected(expr.Row(ids)))
	nullable := rows.SelectScalar(rows.From(table.Query().Where(orm.KeyEq(table.Key(), 2))), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(table.Note)))))
	project := rows.OrderBy(rows.Select(source, fields, rows.Map(fields.ID, ids), rows.MapSelected(fields.Missing, expr.AsSelected(rows.ScalarRowQuery(source, missing))), rows.MapSelected(fields.Note, expr.AsSelected(rows.ScalarNullableRowQuery(source, nullable)))), expr.Asc(ids))
	parentIDs := rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.Value(orm.NullableColumnExpr(table.ParentID)))))
	got, err := rows.Read(project.Where(rows.InNullableQuery(ids, parentIDs))).All(ctx, db)
	must(t, err)
	if len(got) != 1 || got[0].ID != 1 || !got[0].Missing.IsNull() || !got[0].Note.IsNull() {
		t.Fatal(got)
	}
	got, err = rows.Read(project.Where(expr.Not(rows.InNullableQuery(ids, parentIDs)))).All(ctx, db)
	must(t, err)
	if len(got) != 0 {
		t.Fatal("NOT IN discarded NULL semantics", got)
	}
	emptyCount := rows.SelectScalar(rows.From(table.Query().Where(orm.KeyEq(table.Key(), 999))), expr.AsSelected(expr.Count[joinNode]()))
	got, err = rows.Read(project.Where(rows.ExistsQuery(source, emptyCount), expr.GreaterNullable(expr.NullableRow(ids), rows.ScalarRowQuery(source, emptyCount)))).All(ctx, db)
	must(t, err)
	if len(got) != 4 {
		t.Fatal("inner aggregate phase or soft deletes changed", got)
	}
	zeroWindow := rows.SelectScalar(rows.From(table.Query().Limit(0)), expr.AsSelected(expr.Count[joinNode]()))
	got, err = rows.Read(project.Where(rows.ExistsQuery(source, zeroWindow))).All(ctx, db)
	must(t, err)
	if len(got) != 0 {
		t.Fatal("EXISTS lost LIMIT zero", got)
	}
	// PostgreSQL owns cardinality failure. The scalar API adds no LIMIT.
	many := rows.SelectScalar(source, expr.AsSelected(expr.Row(ids)))
	bad := rows.Select(source, fields, rows.Map(fields.ID, ids), rows.MapSelected(fields.Missing, expr.AsSelected(rows.ScalarRowQuery(source, many))), rows.MapSelected(fields.Note, expr.AsSelected(rows.ScalarNullableRowQuery(source, nullable))))
	got, err = rows.Read(bad).All(ctx, db)
	if err == nil || got != nil {
		t.Fatal("multiple-row scalar succeeded", got, err)
	}
	var sqlState interface{ SQLState() string }
	if !errors.As(err, &sqlState) || sqlState.SQLState() != "21000" {
		t.Fatal("wrong scalar cardinality error", err)
	}
	a, b := rows.As[joinLeft](table.Query().Where(orm.KeyNe(table.Key(), 5)), "a"), rows.As[joinRight](table.Query(), "b")
	on := rows.On(expr.At(rows.Scope(a), orm.NullableColumnExpr(table.ParentID)), expr.At(rows.Scope(b), orm.KeyExpr(table.Key())))
	filtered := rows.LeftJoin(a, b, on.WhereRight(expr.IsNull(expr.At(rows.Scope(b), orm.NullableTextExpr(table.Note)))))
	l, r := rows.LeftScope(filtered, rows.Scope(a)), rows.NullableRightScope(filtered, rows.Scope(b))
	onQuery := rows.Select(filtered, fields, rows.Map(fields.ID, expr.At(l, orm.KeyExpr(table.Key()))), rows.Map(fields.Missing, expr.AtNullable(r, orm.KeyExpr(table.Key()))), rows.Map(fields.Note, expr.AtNullableValue(r, orm.NullableTextExpr(table.Note))))
	got, err = rows.Read(onQuery).All(ctx, db)
	must(t, err)
	if len(got) != 3 {
		t.Fatal("ON filter removed preserved rows", got)
	}
	for _, v := range got {
		if !v.Missing.IsNull() {
			t.Fatal("ON filter admitted root note", got)
		}
	}
	plain := rows.LeftJoin(a, b, on)
	pl, pr := rows.LeftScope(plain, rows.Scope(a)), rows.NullableRightScope(plain, rows.Scope(b))
	got, err = rows.Read(rows.Select(plain, fields, rows.Map(fields.ID, expr.At(pl, orm.KeyExpr(table.Key()))), rows.Map(fields.Missing, expr.AtNullable(pr, orm.KeyExpr(table.Key()))), rows.Map(fields.Note, expr.AtNullableValue(pr, orm.NullableTextExpr(table.Note)))).Where(expr.IsNull(expr.AtNullableValue(pr, orm.NullableTextExpr(table.Note))))).All(ctx, db)
	must(t, err)
	if len(got) != 2 {
		t.Fatal("WHERE/ON outer-join distinction lost", got)
	}
}
