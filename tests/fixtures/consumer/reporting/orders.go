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
	joined := rows.InnerJoin(members, orders, rows.On(expr.At(m, MemberModels.ID), expr.At(o, OrderModels.MemberID)))
	member := rows.LeftScope(joined, m)
	order := rows.RightScope(joined, o)
	wire := OrderRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	base := rows.Select(joined, OrderRows,
		rows.Map(OrderRows.ID, expr.At(order, OrderModels.ID)), rows.Map(OrderRows.MemberName, expr.At(member, MemberModels.Name)),
		rows.Map(OrderRows.Label, expr.At(order, OrderModels.Label)), rows.Map(OrderRows.Amount, expr.At(order, OrderModels.Amount)))
	return datatable.Spec[OrderScope, OrderRow, Authority]{
		ID: "reports.orders", Row: OrderRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[OrderScope, OrderRow]{
			datatable.DefineColumn[OrderScope](wire.ID, OrderIDLabel).ExportAs(datatable.ScalarCell(foundryhttp.ModelIDQuery[Order]())).Registration(),
			datatable.DefineColumn[OrderScope](wire.MemberName, MemberNameLabel).SortBy(expr.Row(expr.At(member, MemberModels.Name)).Value()).FilterBy(datatable.Where(text, expr.TextRowAt(member, MemberModels.Name))).Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
			datatable.DefineColumn[OrderScope](wire.Label, OrderLabel).FilterBy(datatable.Where(text, expr.TextRowAt(order, OrderModels.Label))).Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
			datatable.DefineColumn[OrderScope](wire.Amount, AmountLabel).SortBy(expr.Row(expr.At(order, OrderModels.Amount)).Value()).FilterBy(datatable.Where(amount, expr.OrderRow(expr.At(order, OrderModels.Amount)))).ExportAs(datatable.ScalarCell(amount)).Registration(),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.MemberName), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[OrderScope]{expr.At(order, OrderModels.ID).Asc()}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[OrderScope, OrderRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[OrderScope, OrderRow]{}, err
			}
			return base.Where(expr.At(member, MemberModels.TenantID).Eq(actor.TenantID), expr.At(order, OrderModels.TenantID).Eq(actor.TenantID)), nil
		},
	}
}

func totalSpec() datatable.Spec[Order, MemberTotal, Authority] {
	fields, wire := OrderModels, MemberTotalValidationFields()
	count, total := expr.Count[Order](), expr.Sum(fields.Amount)
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberID := foundryhttp.ModelIDQuery[Member]()
	return datatable.Spec[Order, MemberTotal, Authority]{
		ID: "reports.totals", Row: MemberTotalJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Order, MemberTotal]{
			datatable.DefineColumn[Order](wire.MemberID, MemberIDLabel).FilterBy(datatable.Where(memberID, expr.CompareRow(expr.Value(fields.MemberID)))).ExportAs(datatable.ScalarCell(memberID)).Registration(),
			datatable.DefineColumn[Order](wire.Count, CountLabel).SortBy(count.Value()).FilterBy(datatable.Having(foundryhttp.IntegerQuery[int64](), count)).ExportAs(datatable.ScalarCell(foundryhttp.IntegerQuery[int64]())).Registration(),
			datatable.DefineColumn[Order](wire.Total, TotalLabel).SortBy(total.Value()).FilterBy(datatable.NullableHaving(amount, total)).ExportAs(datatable.NullableCell(amount)).Registration(),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.Total), Direction: datatable.Descending}},
		Stable:      []expr.ProjectionOrder[Order]{expr.Value(fields.MemberID).Asc()}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[Order, MemberTotal], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[Order, MemberTotal]{}, err
			}
			source := OrderModels.Query().Where(fields.TenantID.Eq(actor.TenantID), orm.Has(OrderModelRelations.Member, MemberModels.TenantID.Eq(actor.TenantID)))
			return rows.GroupBy(rows.Select(rows.From(source), MemberTotals,
				rows.Map(MemberTotals.MemberID, expr.Value(fields.MemberID)), rows.MapSelected(MemberTotals.Count, count.Value()), rows.MapSelected(MemberTotals.Total, total.Value())), expr.Group(expr.Value(fields.MemberID))), nil
		},
	}
}
