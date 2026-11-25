package reporting

import "github.com/weiloon1234/Foundry-Go/validation"

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

const (
	OrderIDLabel i18n.MessageKey = "reports.order.id"
	AmountLabel  i18n.MessageKey = "reports.order.amount"
	CountLabel   i18n.MessageKey = "reports.total.count"
	TotalLabel   i18n.MessageKey = "reports.total.amount"
)

var Orders = datatable.Define(orderSpec())
var Totals = datatable.Define(totalSpec())

// doc-example:begin joined-table
type memberSide struct{}
type orderSide struct{}
type OrderScope = rows.Inner[rows.Alias[memberSide, Member], rows.Alias[orderSide, Order]]

// orderJoin joins members to their orders and returns the joined source with a
// scope for each side.
func orderJoin() (rows.SelectionSource[OrderScope], expr.Scope[OrderScope, Member], expr.Scope[OrderScope, Order]) {
	members := rows.As[memberSide](MemberModels.Query(), "members")
	orders := rows.As[orderSide](OrderModels.Query(), "orders")
	m, o := rows.Scope(members), rows.Scope(orders)
	joined := rows.InnerJoin(members, orders, rows.On(expr.At(m, MemberModels.ID), expr.At(o, OrderModels.MemberID)))
	return joined, rows.LeftScope(joined, m), rows.RightScope(joined, o)
}

func orderSpec() datatable.Spec[OrderScope, OrderRow, Authority] {
	joined, member, order := orderJoin()
	wire := OrderRowColumns()
	text := foundryhttp.StringQuery[string]()
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberName := expr.TextRowAt(member, MemberModels.Name)
	orderLabel := expr.TextRowAt(order, OrderModels.Label)
	id := expr.Row(expr.At(order, OrderModels.ID))
	orderAmount := expr.OrderRow(expr.At(order, OrderModels.Amount))
	return datatable.Spec[OrderScope, OrderRow, Authority]{
		ID: "reports.orders", Row: OrderRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[OrderScope, OrderRow]{
			datatable.Column(wire.ID, OrderIDLabel, expr.AsSelected(id), datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Order]()))),
			datatable.Column(wire.MemberName, MemberNameLabel, expr.AsSelected(memberName), datatable.SortBy(expr.AsSelected(expr.Row(memberName))), datatable.FilterBy(datatable.Where(text, expr.TextFilter(memberName))), datatable.Searchable(memberName), datatable.ExportAs(expr.AsSelected(memberName), datatable.ScalarCell(text))),
			datatable.Column(wire.Label, OrderLabel, expr.AsSelected(orderLabel), datatable.FilterBy(datatable.Where(text, expr.TextFilter(orderLabel))), datatable.Searchable(orderLabel), datatable.ExportAs(expr.AsSelected(orderLabel), datatable.ScalarCell(text))),
			datatable.Column(wire.Amount, AmountLabel, expr.AsSelected(orderAmount), datatable.SortBy(expr.AsSelected(orderAmount)), datatable.FilterBy(datatable.Where(amount, expr.OrderFilter(orderAmount))), datatable.ExportAs(expr.AsSelected(orderAmount), datatable.ScalarCell(amount))),
		},
		DefaultSort: []datatable.Sort{{Column: validation.BindingName(wire.MemberName), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[OrderScope]{expr.Asc(expr.At(order, OrderModels.ID))}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.SelectionSource[OrderScope], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return nil, err
			}
			return rows.WhereSource(joined, expr.Eq(expr.At(member, MemberModels.TenantID), actor.TenantID), expr.Eq(expr.At(order, OrderModels.TenantID), actor.TenantID)), nil
		},
	}
}

// doc-example:end joined-table

// doc-example:begin grouped-table
func totalSpec() datatable.Spec[Order, MemberTotal, Authority] {
	fields, wire := OrderModels, MemberTotalColumns()
	count, total := expr.Count[Order](), expr.Sum(fields.Amount)
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberID := foundryhttp.ModelIDQuery[Member]()
	member := expr.CompareRow(expr.Value(fields.MemberID))
	return datatable.Spec[Order, MemberTotal, Authority]{
		ID: "reports.totals", Row: MemberTotalJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Order, MemberTotal]{
			datatable.Column(wire.MemberID, MemberIDLabel, expr.AsSelected(member), datatable.FilterBy(datatable.Where(memberID, expr.CompareFilter(member))), datatable.ExportAs(expr.AsSelected(member), datatable.ScalarCell(memberID))),
			datatable.Column(wire.Count, CountLabel, expr.AsSelected(count), datatable.SortBy(expr.AsSelected(count)), datatable.FilterBy(datatable.Having(foundryhttp.IntegerQuery[int64](), expr.OrderValueFilter(count))), datatable.ExportAs(expr.AsSelected(count), datatable.ScalarCell(foundryhttp.IntegerQuery[int64]()))),
			datatable.Column(wire.Total, TotalLabel, expr.AsSelected(total), datatable.SortBy(expr.AsSelected(total)), datatable.FilterBy(datatable.NullableHaving(amount, expr.NullableOrderValueFilter(total))), datatable.ExportAs(expr.AsSelected(total), datatable.NullableCell(amount))),
		},
		DefaultSort: []datatable.Sort{{Column: validation.BindingName(wire.Total), Direction: datatable.Descending}},
		Stable:      []expr.ProjectionOrder[Order]{expr.Asc(expr.Value(fields.MemberID))}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.SelectionSource[Order], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return nil, err
			}
			source := OrderModels.Query().Where(orm.Eq(fields.TenantID, actor.TenantID), orm.Has(OrderModelRelations.Member, orm.Eq(MemberModels.TenantID, actor.TenantID)))
			return rows.GroupSource(rows.From(source), expr.Group(expr.Value(fields.MemberID))), nil
		},
	}
}

// doc-example:end grouped-table
