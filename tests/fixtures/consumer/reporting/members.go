package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/query"
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

var Members = memberTable()

func memberTable() datatable.Table[Member, MemberRow, Authority] {
	fields, wire := MemberFields(), MemberRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	state := foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor())
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	name := query.Trim(fields.Name)
	return datatable.Define(datatable.Spec[Member, MemberRow, Authority]{
		ID: "reports.members", Row: MemberRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Member, MemberRow]{
			datatable.DefineColumn[Member](wire.ID, MemberIDLabel).SortBy(fields.ID.Value()).ExportAs(datatable.ScalarCell(foundryhttp.ModelIDQuery[Member]())).Registration(),
			datatable.DefineColumn[Member](wire.Name, MemberNameLabel).SortBy(name.Value()).FilterBy(datatable.Where(text, name)).Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
			datatable.DefineColumn[Member](wire.Nickname, NicknameLabel).SortBy(fields.Nickname.Value()).FilterBy(datatable.NullableWhere(text, fields.Nickname)).ExportAs(datatable.NullableCell(text)).Registration(),
			datatable.DefineColumn[Member](wire.State, StateLabel).FilterBy(datatable.Where(state, fields.State)).ExportAs(datatable.ScalarCell(state)).Registration(),
			datatable.DefineColumn[Member](wire.Balance, BalanceLabel).SortBy(fields.Balance.Value()).FilterBy(datatable.Where(amount, fields.Balance)).ExportAs(datatable.ScalarCell(amount)).Registration(),
		},
		Filters: []datatable.FilterRegistration[Member]{
			datatable.DefineFilter("orderLabel", OrderLabel, datatable.Related(datatable.Where(text, OrderFields().Label), func(predicate query.Predicate[Order]) query.Predicate[Member] {
				return MemberRelations().Orders.Where(predicate).Exists()
			})),
		},
		DefaultSort: []datatable.Sort{{Column: wire.Name.Name(), Direction: datatable.Ascending}},
		Stable:      []query.ProjectionOrder[Member]{fields.ID.Asc()},
		Authorize:   authorize,
		Source: func(ctx context.Context, authority Authority) (query.ProjectionQuery[Member, MemberRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return query.ProjectionQuery[Member, MemberRow]{}, err
			}
			return SelectMemberRow(QueryReportMembers().Where(fields.TenantID.Eq(actor.TenantID)), MemberRowSelection[Member]{ID: fields.ID.Value(), Name: name.Value(), Nickname: fields.Nickname.Value(), State: fields.State.Value(), Balance: fields.Balance.Value()}), nil
		},
	})
}
