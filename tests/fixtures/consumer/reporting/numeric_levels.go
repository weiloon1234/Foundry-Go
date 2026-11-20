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

//foundry:dto role=row
type NumericLevelRow struct {
	ID        model.ID[Member] `json:"id"`
	Level     AgentLevel       `json:"level"`
	WideLevel WideLevel        `json:"wideLevel"`
}

var NumericLevels = datatable.Define(numericLevelSpec())

func numericLevelSpec() datatable.Spec[Member, NumericLevelRow, Authority] {
	fields, wire := MemberModels, NumericLevelRowColumns()
	id := expr.Row(expr.Value(fields.ID))
	level := expr.AsSelected(expr.Constant[Member](AgentLevelCodec(), MemberLevel))
	wideLevel := expr.AsSelected(expr.Constant[Member](WideLevelCodec(), WideMemberLevel))
	return datatable.Spec[Member, NumericLevelRow, Authority]{
		ID: "reports.numeric_levels", Row: NumericLevelRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Member, NumericLevelRow]{
			datatable.Column(wire.ID, MemberIDLabel, expr.AsSelected(id), datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Member]()))),
			datatable.Column(wire.Level, "reports.numeric_levels.level", level, datatable.ExportAs(level, datatable.ScalarCell(foundryhttp.EnumQuery[AgentLevel, *AgentLevel](MemberLevel.EnumDescriptor())))),
			datatable.Column(wire.WideLevel, "reports.numeric_levels.wide_level", wideLevel, datatable.ExportAs(wideLevel, datatable.ScalarCell(foundryhttp.EnumQuery[WideLevel, *WideLevel](WideMemberLevel.EnumDescriptor())))),
		},
		Stable:    []expr.ProjectionOrder[Member]{expr.Asc(expr.Value(fields.ID))},
		Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.SelectionSource[Member], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return nil, err
			}
			return rows.From(fields.Query().Where(orm.Eq(fields.TenantID, actor.TenantID))), nil
		},
	}
}
