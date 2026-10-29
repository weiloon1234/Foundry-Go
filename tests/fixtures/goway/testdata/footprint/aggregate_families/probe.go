package aggregate_families

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Model struct {
	ID     int64
	Exact  value.Nullable[int64]
	Float  value.Nullable[float64]
	Period value.Nullable[temporal.Interval]
	Name   value.Nullable[string]
}
type fields struct {
	orm.Table[Model, int64]
	Exact  orm.NullableNumber[Model, int64]
	Float  orm.NullableNumber[Model, float64]
	Period orm.NullableInterval[Model]
	Name   orm.NullableText[Model, string]
}

var table = orm.Define("samples", func(b orm.Builder[Model], r *Model) fields {
	return fields{orm.TableOf(b, &r.ID), orm.NullableNumberOf(b, &r.Exact), orm.NullableNumberOf(b, &r.Float), orm.NullableIntervalOf(b, &r.Period), orm.NullableTextOf(b, &r.Name)}
})

type Result struct {
	Exact  value.Nullable[decimal.Decimal]
	Float  value.Nullable[float64]
	Period value.Nullable[temporal.Interval]
	Name   value.Nullable[string]
}
type resultFields struct {
	rows.Row[Result]
	Exact  rows.Field[Result, value.Nullable[decimal.Decimal]]
	Float  rows.Field[Result, value.Nullable[float64]]
	Period rows.Field[Result, value.Nullable[temporal.Interval]]
	Name   rows.Field[Result, value.Nullable[string]]
}

var schema = rows.Define(func(b rows.Builder[Result], r *Result) resultFields {
	return resultFields{b.Row(), rows.NullableNumberOf(b, &r.Exact), rows.NullableNumberOf(b, &r.Float), rows.NullableFieldOf(b, &r.Period, orm.Codec(codec.Interval())), rows.NullableTextOf(b, &r.Name)}
})

type side struct{}

func Query() rows.Query[rows.Alias[side, Model], Result] {
	a := rows.As[side](table.Query(), "s")
	scope := rows.Scope(a)
	return rows.Select(a, schema, rows.MapSelected(schema.Exact, expr.AvgAt(scope, orm.NullableNumberExpr(table.Exact)).Value()), rows.MapSelected(schema.Float, expr.SumFloatAt(scope, orm.NullableNumberExpr(table.Float)).Value()), rows.MapSelected(schema.Period, expr.AvgIntervalAt(scope, orm.NullableIntervalExpr(table.Period)).Value()), rows.MapSelected(schema.Name, expr.MaxAt(scope, orm.NullableTextExpr(table.Name)).Value()))
}
