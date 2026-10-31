package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:enum labels=reports.agent_level
type AgentLevel int32

const (
	MemberLevel AgentLevel = 0
	LeaderLevel AgentLevel = 1
)

//foundry:enum labels=reports.wide_level
type WideLevel int64

const (
	WideMemberLevel WideLevel = 9007199254740993
	WideLeaderLevel WideLevel = 9007199254740994
)

//foundry:dto
type NumericLevelRow struct {
	ID        model.ID[Member] `json:"id"`
	Level     AgentLevel       `json:"level"`
	WideLevel WideLevel        `json:"wideLevel"`
}

type NumericLevelRowColumns struct {
	rows.Row[NumericLevelRow]
	ID        rows.Field[NumericLevelRow, model.ID[Member]]
	Level     rows.Field[NumericLevelRow, AgentLevel]
	WideLevel rows.Field[NumericLevelRow, WideLevel]
}

var NumericLevelRows = rows.Define(func(b rows.Builder[NumericLevelRow], r *NumericLevelRow) NumericLevelRowColumns {
	return NumericLevelRowColumns{b.Row(), rows.IDOf(b, &r.ID), rows.EnumOf(b, &r.Level), rows.EnumOf(b, &r.WideLevel)}
})

var NumericLevels = datatable.Define(numericLevelSpec())

func numericLevelSpec() datatable.Spec[Member, NumericLevelRow, Authority] {
	fields, wire := MemberModels, NumericLevelRowValidationFields()
	id := expr.Row(expr.Value(orm.KeyExpr(fields.ID)))
	level := expr.AsSelected(expr.Constant[Member](AgentLevelCodec(), MemberLevel))
	wideLevel := expr.AsSelected(expr.Constant[Member](WideLevelCodec(), WideMemberLevel))
	return datatable.Spec[Member, NumericLevelRow, Authority]{
		ID: "reports.numeric_levels", Row: NumericLevelRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Member, NumericLevelRow]{
			datatable.DefineColumn[Member](wire.ID, MemberIDLabel, datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Member]()))),
			datatable.DefineColumn[Member](wire.Level, "reports.numeric_levels.level", datatable.ExportAs(level, datatable.ScalarCell(foundryhttp.EnumQuery[AgentLevel, *AgentLevel](MemberLevel.EnumDescriptor())))),
			datatable.DefineColumn[Member](wire.WideLevel, "reports.numeric_levels.wide_level", datatable.ExportAs(wideLevel, datatable.ScalarCell(foundryhttp.EnumQuery[WideLevel, *WideLevel](WideMemberLevel.EnumDescriptor())))),
		},
		Stable:    []expr.ProjectionOrder[Member]{expr.Asc(expr.Value(orm.KeyExpr(fields.ID)))},
		Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[Member, NumericLevelRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[Member, NumericLevelRow]{}, err
			}
			return rows.Select(rows.From(fields.Query().Where(orm.NumberEq(fields.TenantID, actor.TenantID))), NumericLevelRows,
				rows.MapSelected(NumericLevelRows.ID, expr.AsSelected(id)), rows.MapSelected(NumericLevelRows.Level, level), rows.MapSelected(NumericLevelRows.WideLevel, wideLevel)), nil
		},
	}
}
