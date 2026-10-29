package derived_aggregates

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type report struct {
	Age      int
	MaybeAge value.Nullable[int]
}
type fields struct {
	rows.Row[report]
	Age      rows.Field[report, int]
	MaybeAge rows.Field[report, value.Nullable[int]]
}

var reports = rows.Define(func(b rows.Builder[report], r *report) fields {
	return fields{b.Row(), rows.NumberOf(b, &r.Age), rows.NullableNumberOf(b, &r.MaybeAge)}
})

type alias struct{}

func Compile() (orm.Statement, error) {
	q := rows.Select(rows.From(models.Users.Query()), reports.Row, rows.Map(reports.Age, expr.Value(orm.NumberExpr(models.Users.Age))), rows.MapSelected(reports.MaybeAge, expr.Row(expr.NullableRow(expr.Value(orm.NumberExpr(models.Users.Age)))).Value()))
	a := rows.AsRecord[alias](rows.RecordOf(q), "totals")
	n, m := rows.At(rows.Scope(a), reports.Age), rows.At(rows.Scope(a), reports.MaybeAge)
	sum := expr.SumRow(n).Filter(expr.OrderRow(n).Gt(0))
	_ = expr.AvgRow(n)
	_ = expr.MinRow(n)
	_ = expr.MaxRow(n)
	_ = expr.CountRow(n)
	_ = expr.CountDistinctRow(m)
	_ = expr.SumNullableRow(m)
	_ = expr.AvgNullableRow(m)
	_ = expr.MinNullableRow(m)
	_ = expr.MaxNullableRow(m)
	return rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, sum.Value())))
}
