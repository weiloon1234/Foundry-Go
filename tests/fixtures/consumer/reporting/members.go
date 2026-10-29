package reporting

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

var Members = datatable.Define(memberSpec())

func memberSpec() datatable.Spec[Member, MemberRow, Authority] {
	fields, wire := MemberModels, MemberRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	state := foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor())
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	name := expr.Trim(expr.Value(fields.Name))
	return datatable.Spec[Member, MemberRow, Authority]{
		ID: "reports.members", Row: MemberRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Member, MemberRow]{
			datatable.DefineColumn[Member](wire.ID, MemberIDLabel).SortBy(expr.Row(expr.Value(fields.ID)).Value()).ExportAs(datatable.ScalarCell(foundryhttp.ModelIDQuery[Member]())).Registration(),
			datatable.DefineColumn[Member](wire.Name, MemberNameLabel).SortBy(name.Value()).FilterBy(datatable.Where(text, name)).Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
			datatable.DefineColumn[Member](wire.Nickname, NicknameLabel).SortBy(expr.Row(expr.Value(fields.Nickname)).Value()).FilterBy(datatable.NullableWhere(text, expr.TextNullableRow(fields.Nickname))).ExportAs(datatable.NullableCell(text)).Registration(),
			datatable.DefineColumn[Member](wire.State, StateLabel).FilterBy(datatable.Where(state, expr.CompareRow(expr.Value(fields.State)))).ExportAs(datatable.ScalarCell(state)).Registration(),
			datatable.DefineColumn[Member](wire.Balance, BalanceLabel).SortBy(expr.Row(expr.Value(fields.Balance)).Value()).FilterBy(datatable.Where(amount, expr.OrderRow(expr.Value(fields.Balance)))).ExportAs(datatable.ScalarCell(amount)).Registration(),
		},
		Filters: []datatable.FilterRegistration[Member]{
			datatable.DefineFilter("orderLabel", OrderLabel, datatable.Related(datatable.Where(text, expr.TextRow(OrderModels.Label)), func(predicate expr.Predicate[Order]) expr.Predicate[Member] {
				return expr.Has(MemberModelRelations.Orders, predicate)
			})),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.Name), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[Member]{expr.Value(fields.ID).Asc()},
		Authorize:   authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[Member, MemberRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[Member, MemberRow]{}, err
			}
			return rows.Select(rows.From(MemberModels.Query().Where(fields.TenantID.Eq(actor.TenantID))), MemberRows,
				rows.Map(MemberRows.ID, expr.Value(fields.ID)), rows.MapSelected(MemberRows.Name, name.Value()),
				rows.Map(MemberRows.Nickname, expr.Value(fields.Nickname)), rows.Map(MemberRows.State, expr.Value(fields.State)), rows.Map(MemberRows.Balance, expr.Value(fields.Balance))), nil
		},
	}
}
