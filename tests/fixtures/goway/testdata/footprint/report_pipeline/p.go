package report_pipeline

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type memberSide struct{}
type orderSide struct{}
type Scope = rows.Inner[rows.Alias[memberSide, reporting.Member], rows.Alias[orderSide, reporting.Order]]

// F mirrors the existing report's joined Source plus its stable order.
func F(tenant int64) rows.Query[Scope, reporting.OrderRow] {
	members := rows.As[memberSide](reporting.Members.Query(), "members")
	orders := rows.As[orderSide](reporting.Orders.Query(), "orders")
	m, o := rows.Scope(members), rows.Scope(orders)
	joined := rows.InnerJoin(members, orders, rows.On(expr.At(m, orm.KeyExpr(reporting.Members.ID)), expr.At(o, orm.ColumnExpr(reporting.Orders.MemberID))))
	member := rows.LeftScope(joined, m)
	order := rows.RightScope(joined, o)
	id := expr.At(order, orm.KeyExpr(reporting.Orders.ID))
	return rows.OrderBy(rows.Select(joined, reporting.OrderRows,
		rows.Map(reporting.OrderRows.ID, id), rows.Map(reporting.OrderRows.MemberName, expr.At(member, orm.TextExpr(reporting.Members.Name))), rows.Map(reporting.OrderRows.Label, expr.At(order, orm.TextExpr(reporting.Orders.Label))), rows.Map(reporting.OrderRows.Amount, expr.At(order, orm.NumberExpr(reporting.Orders.Amount))),
	).Where(expr.At(member, orm.NumberExpr(reporting.Members.TenantID)).Eq(tenant), expr.At(order, orm.NumberExpr(reporting.Orders.TenantID)).Eq(tenant)), id.Asc())
}
