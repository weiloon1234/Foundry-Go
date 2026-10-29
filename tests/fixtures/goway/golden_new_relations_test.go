package goway_test

import (
	"context"
	"fmt"
	"foundry.test/goway/models"
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	ormrows "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (g newGolden) stage1b(ctx context.Context, db *database.DB) {
	check(orm.Check(models.OrderRelations.Buyer, models.UserRelations.Orders, models.UserRelations.Introducer, reporting.Members, reporting.Orders))
	check(ormrows.Check(reporting.OrderRows))
	fmt.Fprintln(&g.output, "eager-buyer")
	orders := must(orm.All(ctx, db, models.Orders.Query().With(models.OrderRelations.Buyer)))
	g.show(orders)
	for _, o := range orders {
		item, loaded := o.Buyer.Get()
		fmt.Fprintf(&g.output, "Buyer loaded: %t\n", loaded)
		g.show(item)
	}
	fmt.Fprintln(&g.output, "eager-orders")
	users := must(orm.All(ctx, db, models.Users.Query().With(models.UserRelations.Orders)))
	g.show(users)
	for _, u := range users {
		items, loaded := u.Orders.Get()
		fmt.Fprintf(&g.output, "Orders loaded: %t\n", loaded)
		g.show(items)
	}
	fmt.Fprintln(&g.output, "nullable-introducer")
	users = must(orm.All(ctx, db, models.Users.Query().With(models.UserRelations.Introducer)))
	for _, u := range users {
		item, loaded := u.Introducer.Get()
		fmt.Fprintf(&g.output, "Introducer loaded: %t\n", loaded)
		g.show(item)
	}
	fmt.Fprintln(&g.output, "nullable-introducer-value")
	g.introducerKey = true
	users = must(orm.All(ctx, db, models.Users.Query().With(models.UserRelations.Introducer)))
	for _, u := range users {
		item, loaded := u.Introducer.Get()
		fmt.Fprintf(&g.output, "Introducer loaded: %t\n", loaded)
		g.show(item)
	}
	g.introducerKey = false
	fmt.Fprintln(&g.output, "report-join-projection")
	g.show(must(ormrows.Read(reporting.SelectOrders(7)).All(ctx, db)))
	fmt.Fprintln(&g.output, "select-value")
	g.show(must(ormrows.Read(ormrows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents))).All(ctx, db)))
	fmt.Fprintln(&g.output, "soft-delete-read")
	g.show(must(orm.All(ctx, db, reporting.Orders.Query())))
	fmt.Fprintln(&g.output, "soft-delete")
	id := must(model.ParseID[reporting.Order](orderID))
	g.show(must(orm.Delete(ctx, db, reporting.Orders.Table.Query(), reporting.Orders.Table.Key(), id)))
	fmt.Fprintln(&g.output, "restore")
	g.show(must(orm.Restore(ctx, db, reporting.Orders.Table, id)))
	g.fixB(ctx, db)
}
