package reporting

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "github.com/weiloon1234/Foundry-Go/validation"

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

const (
	MemberIDLabel   i18n.MessageKey = "reports.member.id"
	MemberNameLabel i18n.MessageKey = "reports.member.name"
	NicknameLabel   i18n.MessageKey = "reports.member.nickname"
	StateLabel      i18n.MessageKey = "reports.member.state"
	BalanceLabel    i18n.MessageKey = "reports.member.balance"
	OrderLabel      i18n.MessageKey = "reports.order.label"
)

// doc-example:begin member-table
var Members = datatable.Define(memberSpec())

func memberSpec() datatable.Spec[Member, MemberRow, Authority] {
	fields, wire := MemberModels, MemberRowColumns()
	text := foundryhttp.StringQuery[string]()
	state := foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor())
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	name := expr.Trim(expr.Value(fields.Name))
	id := expr.Row(expr.Value(fields.ID))
	return datatable.Spec[Member, MemberRow, Authority]{
		ID: "reports.members", Row: MemberRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Member, MemberRow]{
			datatable.Column(wire.ID, MemberIDLabel, expr.AsSelected(id), datatable.SortBy(expr.AsSelected(id)), datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Member]()))),
			datatable.Column(wire.Name, MemberNameLabel, expr.AsSelected(name), datatable.SortBy(expr.AsSelected(name)), datatable.FilterBy(datatable.Where(text, expr.TextFilter(name))), datatable.Searchable(name), datatable.ExportAs(expr.AsSelected(name), datatable.ScalarCell(text))),
			datatable.NullableText(wire.Nickname, NicknameLabel, fields.Nickname, datatable.NoSearch),
			datatable.Enum(wire.State, StateLabel, fields.State, state, datatable.NoSort),
			datatable.Number(wire.Balance, BalanceLabel, fields.Balance, amount),
		},
		Filters: []datatable.FilterRegistration[Member]{
			datatable.DefineFilter("orderLabel", OrderLabel, datatable.Related(datatable.Where(text, expr.TextFilter(expr.TextRow(OrderModels.Label))), func(predicate expr.Predicate[Order]) expr.Predicate[Member] {
				return expr.Has(MemberModelRelations.Orders, predicate)
			})),
		},
		DefaultSort: []datatable.Sort{{Column: validation.BindingName(wire.Name), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[Member]{expr.Asc(expr.Value(fields.ID))},
		Authorize:   authorize,
		Source: func(ctx context.Context, authority Authority) (rows.SelectionSource[Member], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return nil, err
			}
			return rows.From(MemberModels.Query().Where(orm.Eq(fields.TenantID, actor.TenantID))), nil
		},
	}
}

// doc-example:end member-table
