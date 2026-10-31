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

var Members = datatable.Define(memberSpec())

func memberSpec() datatable.Spec[Member, MemberRow, Authority] {
	fields, wire := MemberModels, MemberRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	state := foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor())
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	name := expr.Trim(expr.Value(orm.TextExpr(fields.Name)))
	id := expr.Row(expr.Value(orm.KeyExpr(fields.ID)))
	nickname := expr.TextNullableRow(orm.NullableTextExpr(fields.Nickname))
	stateExpr := expr.CompareRow(expr.Value(orm.EnumExpr(fields.State)))
	balance := expr.OrderRow(expr.Value(orm.NumberExpr(fields.Balance)))
	return datatable.Spec[Member, MemberRow, Authority]{
		ID: "reports.members", Row: MemberRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Member, MemberRow]{
			datatable.DefineColumn[Member](wire.ID, MemberIDLabel, datatable.SortBy(expr.AsSelected(id)), datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Member]()))),
			datatable.DefineColumn[Member](wire.Name, MemberNameLabel, datatable.SortBy(expr.AsSelected(name)), datatable.FilterBy(datatable.Where(text, expr.TextFilter(name))), datatable.Searchable(name), datatable.ExportAs(expr.AsSelected(name), datatable.ScalarCell(text))),
			datatable.DefineColumn[Member](wire.Nickname, NicknameLabel, datatable.SortBy(expr.AsSelected(nickname)), datatable.FilterBy(datatable.NullableWhere(text, expr.NullableTextFilter(nickname))), datatable.ExportAs(expr.AsSelected(nickname), datatable.NullableCell(text))),
			datatable.DefineColumn[Member](wire.State, StateLabel, datatable.FilterBy(datatable.Where(state, expr.CompareFilter(stateExpr))), datatable.ExportAs(expr.AsSelected(stateExpr), datatable.ScalarCell(state))),
			datatable.DefineColumn[Member](wire.Balance, BalanceLabel, datatable.SortBy(expr.AsSelected(balance)), datatable.FilterBy(datatable.Where(amount, expr.OrderFilter(balance))), datatable.ExportAs(expr.AsSelected(balance), datatable.ScalarCell(amount))),
		},
		Filters: []datatable.FilterRegistration[Member]{
			datatable.DefineFilter("orderLabel", OrderLabel, datatable.Related(datatable.Where(text, expr.TextFilter(expr.TextRow(orm.TextExpr(OrderModels.Label)))), func(predicate expr.Predicate[Order]) expr.Predicate[Member] {
				return expr.Has(MemberModelRelations.Orders, predicate)
			})),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.Name), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[Member]{expr.Asc(expr.Value(orm.KeyExpr(fields.ID)))},
		Authorize:   authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[Member, MemberRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[Member, MemberRow]{}, err
			}
			return rows.Select(rows.From(MemberModels.Query().Where(orm.NumberEq(fields.TenantID, actor.TenantID))), MemberRows,
				rows.Map(MemberRows.ID, expr.Value(orm.KeyExpr(fields.ID))), rows.MapSelected(MemberRows.Name, expr.AsSelected(name)),
				rows.Map(MemberRows.Nickname, expr.Value(orm.NullableTextExpr(fields.Nickname))), rows.Map(MemberRows.State, expr.Value(orm.EnumExpr(fields.State))), rows.Map(MemberRows.Balance, expr.Value(orm.NumberExpr(fields.Balance)))), nil
		},
	}
}
