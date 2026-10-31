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

type memberSide struct{}
type orderSide struct{}
type OrderScope = rows.Inner[rows.Alias[memberSide, Member], rows.Alias[orderSide, Order]]

const (
	OrderIDLabel i18n.MessageKey = "reports.order.id"
	AmountLabel  i18n.MessageKey = "reports.order.amount"
	CountLabel   i18n.MessageKey = "reports.total.count"
	TotalLabel   i18n.MessageKey = "reports.total.amount"
)

var Orders = datatable.Define(orderSpec())
var Totals = datatable.Define(totalSpec())

func orderSpec() datatable.Spec[OrderScope, OrderRow, Authority] {
	members := rows.As[memberSide](MemberModels.Query(), "members")
	orders := rows.As[orderSide](OrderModels.Query(), "orders")
	m, o := rows.Scope(members), rows.Scope(orders)
	joined := rows.InnerJoin(members, orders, rows.On(expr.At(m, orm.KeyExpr(MemberModels.ID)), expr.At(o, orm.ColumnExpr(OrderModels.MemberID))))
	member := rows.LeftScope(joined, m)
	order := rows.RightScope(joined, o)
	wire := OrderRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberName := expr.TextRowAt(member, orm.TextExpr(MemberModels.Name))
	orderLabel := expr.TextRowAt(order, orm.TextExpr(OrderModels.Label))
	id := expr.Row(expr.At(order, orm.KeyExpr(OrderModels.ID)))
	orderAmount := expr.OrderRow(expr.At(order, orm.NumberExpr(OrderModels.Amount)))
	base := rows.Select(joined, OrderRows,
		rows.Map(OrderRows.ID, expr.At(order, orm.KeyExpr(OrderModels.ID))), rows.Map(OrderRows.MemberName, expr.At(member, orm.TextExpr(MemberModels.Name))),
		rows.Map(OrderRows.Label, expr.At(order, orm.TextExpr(OrderModels.Label))), rows.Map(OrderRows.Amount, expr.At(order, orm.NumberExpr(OrderModels.Amount))))
	return datatable.Spec[OrderScope, OrderRow, Authority]{
		ID: "reports.orders", Row: OrderRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[OrderScope, OrderRow]{
			datatable.DefineColumn[OrderScope](wire.ID, OrderIDLabel, datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Order]()))),
			datatable.DefineColumn[OrderScope](wire.MemberName, MemberNameLabel, datatable.SortBy(expr.AsSelected(expr.Row(memberName))), datatable.FilterBy(datatable.Where(text, expr.TextFilter(memberName))), datatable.Searchable(memberName), datatable.ExportAs(expr.AsSelected(memberName), datatable.ScalarCell(text))),
			datatable.DefineColumn[OrderScope](wire.Label, OrderLabel, datatable.FilterBy(datatable.Where(text, expr.TextFilter(orderLabel))), datatable.Searchable(orderLabel), datatable.ExportAs(expr.AsSelected(orderLabel), datatable.ScalarCell(text))),
			datatable.DefineColumn[OrderScope](wire.Amount, AmountLabel, datatable.SortBy(expr.AsSelected(orderAmount)), datatable.FilterBy(datatable.Where(amount, expr.OrderFilter(orderAmount))), datatable.ExportAs(expr.AsSelected(orderAmount), datatable.ScalarCell(amount))),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.MemberName), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[OrderScope]{expr.Asc(expr.At(order, orm.KeyExpr(OrderModels.ID)))}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[OrderScope, OrderRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[OrderScope, OrderRow]{}, err
			}
			return base.Where(expr.Eq(expr.At(member, orm.NumberExpr(MemberModels.TenantID)), actor.TenantID), expr.Eq(expr.At(order, orm.NumberExpr(OrderModels.TenantID)), actor.TenantID)), nil
		},
	}
}

func totalSpec() datatable.Spec[Order, MemberTotal, Authority] {
	fields, wire := OrderModels, MemberTotalValidationFields()
	count, total := expr.Count[Order](), expr.Sum(orm.NumberExpr(fields.Amount))
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberID := foundryhttp.ModelIDQuery[Member]()
	member := expr.CompareRow(expr.Value(orm.ColumnExpr(fields.MemberID)))
	return datatable.Spec[Order, MemberTotal, Authority]{
		ID: "reports.totals", Row: MemberTotalJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Order, MemberTotal]{
			datatable.DefineColumn[Order](wire.MemberID, MemberIDLabel, datatable.FilterBy(datatable.Where(memberID, expr.CompareFilter(member))), datatable.ExportAs(expr.AsSelected(member), datatable.ScalarCell(memberID))),
			datatable.DefineColumn[Order](wire.Count, CountLabel, datatable.SortBy(expr.AsSelected(count)), datatable.FilterBy(datatable.Having(foundryhttp.IntegerQuery[int64](), expr.OrderValueFilter(count))), datatable.ExportAs(expr.AsSelected(count), datatable.ScalarCell(foundryhttp.IntegerQuery[int64]()))),
			datatable.DefineColumn[Order](wire.Total, TotalLabel, datatable.SortBy(expr.AsSelected(total)), datatable.FilterBy(datatable.NullableHaving(amount, expr.NullableOrderValueFilter(total))), datatable.ExportAs(expr.AsSelected(total), datatable.NullableCell(amount))),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.Total), Direction: datatable.Descending}},
		Stable:      []expr.ProjectionOrder[Order]{expr.Asc(expr.Value(orm.ColumnExpr(fields.MemberID)))}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[Order, MemberTotal], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[Order, MemberTotal]{}, err
			}
			source := OrderModels.Query().Where(orm.NumberEq(fields.TenantID, actor.TenantID), orm.Has(OrderModelRelations.Member, orm.NumberEq(MemberModels.TenantID, actor.TenantID)))
			return rows.GroupBy(rows.Select(rows.From(source), MemberTotals,
				rows.Map(MemberTotals.MemberID, expr.Value(orm.ColumnExpr(fields.MemberID))), rows.MapSelected(MemberTotals.Count, expr.AsSelected(count)), rows.MapSelected(MemberTotals.Total, expr.AsSelected(total))), expr.Group(expr.Value(orm.ColumnExpr(fields.MemberID)))), nil
		},
	}
}
