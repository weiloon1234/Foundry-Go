package reporting

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
)

// OrderRow keeps the fixture's plain DTO shape and JSON field names.
type OrderRow struct {
	ID         model.ID[Order] `json:"id"`
	MemberName string          `json:"memberName"`
	Label      string          `json:"label"`
	Amount     decimal.Decimal `json:"amount"`
}
type OrderRowFields struct {
	rows.Row[OrderRow]
	ID         rows.Field[OrderRow, model.ID[Order]]
	MemberName rows.Field[OrderRow, string]
	Label      rows.Field[OrderRow, string]
	Amount     rows.Field[OrderRow, decimal.Decimal]
}

var OrderRows = rows.Define(func(b rows.Builder[OrderRow], r *OrderRow) OrderRowFields {
	return OrderRowFields{Row: b.Row(), ID: rows.IDOf(b, &r.ID), MemberName: rows.TextOf(b, &r.MemberName), Label: rows.TextOf(b, &r.Label), Amount: rows.NumberOf(b, &r.Amount)}
})

type memberSide struct{}
type orderSide struct{}
type OrderScope = rows.Inner[rows.Alias[memberSide, Member], rows.Alias[orderSide, Order]]

// SelectOrders mirrors the existing report's joined Source plus its stable order.
func SelectOrders(tenant int64) rows.Query[OrderScope, OrderRow] {
	members := rows.As[memberSide](Members.Query(), "members")
	orders := rows.As[orderSide](Orders.Query(), "orders")
	m, o := rows.Scope(members), rows.Scope(orders)
	joined := rows.InnerJoin(members, orders, rows.On(expr.At(m, Members.ID), expr.At(o, Orders.MemberID)))
	member := rows.LeftScope(joined, m)
	order := rows.RightScope(joined, o)
	id := expr.At(order, Orders.ID)
	return rows.Select(joined, OrderRows,
		rows.Map(OrderRows.ID, id), rows.Map(OrderRows.MemberName, expr.At(member, Members.Name)), rows.Map(OrderRows.Label, expr.At(order, Orders.Label)), rows.Map(OrderRows.Amount, expr.At(order, Orders.Amount)),
	).Where(expr.At(member, Members.TenantID).Eq(tenant), expr.At(order, Orders.TenantID).Eq(tenant)).OrderBy(id.Asc())
}
