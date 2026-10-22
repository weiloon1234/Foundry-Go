package goway_test

import (
	"context"
	"fmt"
	models "foundry.test/goway/oldmodels"
	reporting "foundry.test/goway/oldreporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (g oldGolden) stage1b(ctx context.Context, db *database.DB) {
	fmt.Fprintln(&g.output, "eager-buyer")
	orders := must(models.QueryOrders().With(models.OrderRelations().Buyer).All(ctx, db))
	g.show(orders)
	for _, o := range orders {
		item, loaded := o.Buyer.Get()
		fmt.Fprintf(&g.output, "Buyer loaded: %t\n", loaded)
		g.show(item)
	}
	fmt.Fprintln(&g.output, "eager-orders")
	users := must(models.QueryUsers().With(models.UserRelations().Orders).All(ctx, db))
	g.show(users)
	for _, u := range users {
		items, loaded := u.Orders.Get()
		fmt.Fprintf(&g.output, "Orders loaded: %t\n", loaded)
		g.show(items)
	}
	fmt.Fprintln(&g.output, "nullable-introducer")
	users = must(models.QueryUsers().With(models.UserRelations().Introducer).All(ctx, db))
	for _, u := range users {
		item, loaded := u.Introducer.Get()
		fmt.Fprintf(&g.output, "Introducer loaded: %t\n", loaded)
		g.show(item)
	}
	fmt.Fprintln(&g.output, "nullable-introducer-value")
	g.introducerKey = true
	users = must(models.QueryUsers().With(models.UserRelations().Introducer).All(ctx, db))
	for _, u := range users {
		item, loaded := u.Introducer.Get()
		fmt.Fprintf(&g.output, "Introducer loaded: %t\n", loaded)
		g.show(item)
	}
	g.introducerKey = false
	fmt.Fprintln(&g.output, "report-join-projection")
	members := query.As[memberSide](reporting.QueryReportMembers(), "members")
	ordersSource := query.As[orderSide](reporting.QueryReportOrders(), "orders")
	m, o := reporting.MemberFieldsAt(members.Scope()), reporting.OrderFieldsAt(ordersSource.Scope())
	joined := query.InnerJoin(members, ordersSource, query.On(m.ID, o.MemberID))
	member := reporting.MemberFieldsAt(query.LeftScope(joined, members.Scope()))
	order := reporting.OrderFieldsAt(query.RightScope(joined, ordersSource.Scope()))
	projection := reporting.SelectOrderRow(joined, reporting.OrderRowSelection[query.Inner[query.Alias[memberSide, reporting.Member], query.Alias[orderSide, reporting.Order]]]{ID: query.Value(order.ID), MemberName: query.Value(member.Name), Label: query.Value(order.Label), Amount: query.Value(order.Amount)})
	g.show(must(projection.Where(member.TenantID.Eq(7), order.TenantID.Eq(7)).OrderBy(order.ID.Asc()).All(ctx, db)))
	fmt.Fprintln(&g.output, "select-value")
	g.show(must(query.SelectValue(models.QueryOrders(), query.Value(models.OrderFields().TotalCents)).All(ctx, db)))
	fmt.Fprintln(&g.output, "soft-delete-read")
	g.show(must(reporting.QueryReportOrders().All(ctx, db)))
	fmt.Fprintln(&g.output, "soft-delete")
	id := must(model.ParseID[reporting.Order](orderID))
	g.show(must(reporting.QueryReportOrders().Delete(ctx, db, id)))
	fmt.Fprintln(&g.output, "restore")
	g.show(must(reporting.QueryReportOrders().Restore(ctx, db, id)))
	g.fixB(ctx, db)
}
