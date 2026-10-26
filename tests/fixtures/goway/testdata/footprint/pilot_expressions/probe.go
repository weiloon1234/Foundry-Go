package pilot_expressions

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type Model struct {
	ID      int64
	Enabled bool
}
type fields struct {
	orm.Table[Model, int64]
	Enabled orm.Column[Model, bool]
}

var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) fields {
	return fields{orm.TableOf(b, &m.ID), orm.BoolOf(b, &m.Enabled)}
})

type Report struct{ Enabled bool }
type columns struct {
	rows.Row[Report]
	Enabled rows.Field[Report, bool]
}

var Reports = rows.Define(func(b rows.Builder[Report], r *Report) columns { return columns{b.Row(), rows.BoolOf(b, &r.Enabled)} })

func Compile() (orm.Statement, error) {
	flag := expr.When(expr.Value(Models.Enabled).Eq(true), expr.Constant[Model](codec.Bool[bool](), true)).Else(expr.Row(expr.Value(Models.Enabled)).Param(false))
	return rows.Compile(rows.Select(rows.From(Models.Query()), Reports.Row, rows.MapSelected(Reports.Enabled, flag.Value())))
}
