package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

type memberSide struct{}
type orderSide struct{}
type OrderScope = query.Inner[query.Alias[memberSide, Member], query.Alias[orderSide, Order]]

const (
	OrderIDLabel i18n.MessageKey = "reports.order.id"
	AmountLabel  i18n.MessageKey = "reports.order.amount"
	CountLabel   i18n.MessageKey = "reports.total.count"
	TotalLabel   i18n.MessageKey = "reports.total.amount"
)

var Orders = orderTable()
var Totals = totalTable()

func orderTable() datatable.Table[OrderScope, OrderRow, Authority] {
	members := query.As[memberSide](QueryReportMembers(), "members")
	orders := query.As[orderSide](QueryReportOrders(), "orders")
	m, o := MemberFieldsAt(members.Scope()), OrderFieldsAt(orders.Scope())
	joined := query.InnerJoin(members, orders, query.On(m.ID, o.MemberID))
	member := MemberFieldsAt(query.LeftScope(joined, members.Scope()))
	order := OrderFieldsAt(query.RightScope(joined, orders.Scope()))
	wire := OrderRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	base := SelectOrderRow(joined, OrderRowSelection[OrderScope]{ID: order.ID.Value(), MemberName: member.Name.Value(), Label: order.Label.Value(), Amount: order.Amount.Value()})
	return datatable.Define(datatable.Spec[OrderScope, OrderRow, Authority]{
		ID: "reports.orders", Row: OrderRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[OrderScope, OrderRow]{
			datatable.DefineColumn[OrderScope](wire.ID, OrderIDLabel).ExportAs(datatable.ScalarCell(foundryhttp.ModelIDQuery[Order]())).Registration(),
			datatable.DefineColumn[OrderScope](wire.MemberName, MemberNameLabel).SortBy(member.Name.Value()).FilterBy(datatable.Where(text, member.Name)).Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
			datatable.DefineColumn[OrderScope](wire.Label, OrderLabel).FilterBy(datatable.Where(text, order.Label)).Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
			datatable.DefineColumn[OrderScope](wire.Amount, AmountLabel).SortBy(order.Amount.Value()).FilterBy(datatable.Where(amount, order.Amount)).ExportAs(datatable.ScalarCell(amount)).Registration(),
		},
		DefaultSort: []datatable.Sort{{Column: wire.MemberName.Name(), Direction: datatable.Ascending}},
		Stable:      []query.ProjectionOrder[OrderScope]{order.ID.Asc()}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (query.ProjectionQuery[OrderScope, OrderRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return query.ProjectionQuery[OrderScope, OrderRow]{}, err
			}
			return base.Where(member.TenantID.Eq(actor.TenantID), order.TenantID.Eq(actor.TenantID)), nil
		},
	})
}

func totalTable() datatable.Table[Order, MemberTotal, Authority] {
	fields, wire := OrderFields(), MemberTotalValidationFields()
	count, total := query.Count[Order](), fields.Amount.Sum()
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberID := foundryhttp.ModelIDQuery[Member]()
	return datatable.Define(datatable.Spec[Order, MemberTotal, Authority]{
		ID: "reports.totals", Row: MemberTotalJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Order, MemberTotal]{
			datatable.DefineColumn[Order](wire.MemberID, MemberIDLabel).FilterBy(datatable.Where(memberID, fields.MemberID)).ExportAs(datatable.ScalarCell(memberID)).Registration(),
			datatable.DefineColumn[Order](wire.Count, CountLabel).SortBy(count.Value()).FilterBy(datatable.Having(foundryhttp.IntegerQuery[int64](), count)).ExportAs(datatable.ScalarCell(foundryhttp.IntegerQuery[int64]())).Registration(),
			datatable.DefineColumn[Order](wire.Total, TotalLabel).SortBy(total.Value()).FilterBy(datatable.NullableHaving(amount, total)).ExportAs(datatable.NullableCell(amount)).Registration(),
		},
		DefaultSort: []datatable.Sort{{Column: wire.Total.Name(), Direction: datatable.Descending}},
		Stable:      []query.ProjectionOrder[Order]{fields.MemberID.Asc()}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (query.ProjectionQuery[Order, MemberTotal], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return query.ProjectionQuery[Order, MemberTotal]{}, err
			}
			source := QueryReportOrders().Where(fields.TenantID.Eq(actor.TenantID), OrderRelations().Member.Where(MemberFields().TenantID.Eq(actor.TenantID)).Exists())
			return SelectMemberTotal(source, MemberTotalSelection[Order]{MemberID: fields.MemberID.Value(), Count: count.Value(), Total: total.Value()}).GroupBy(fields.MemberID.Group()), nil
		},
	})
}
