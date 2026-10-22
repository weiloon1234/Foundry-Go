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
	orders := must(models.Orders.Query().With(models.OrderRelations.Buyer).All(ctx, db))
	g.show(orders)
	for _, o := range orders {
		item, loaded := o.Buyer.Get()
		fmt.Fprintf(&g.output, "Buyer loaded: %t\n", loaded)
		g.show(item)
	}
	fmt.Fprintln(&g.output, "eager-orders")
	users := must(models.Users.Query().With(models.UserRelations.Orders).All(ctx, db))
	g.show(users)
	for _, u := range users {
		items, loaded := u.Orders.Get()
		fmt.Fprintf(&g.output, "Orders loaded: %t\n", loaded)
		g.show(items)
	}
	fmt.Fprintln(&g.output, "nullable-introducer")
	users = must(models.Users.Query().With(models.UserRelations.Introducer).All(ctx, db))
	for _, u := range users {
		item, loaded := u.Introducer.Get()
		fmt.Fprintf(&g.output, "Introducer loaded: %t\n", loaded)
		g.show(item)
	}
	fmt.Fprintln(&g.output, "nullable-introducer-value")
	g.introducerKey = true
	users = must(models.Users.Query().With(models.UserRelations.Introducer).All(ctx, db))
	for _, u := range users {
		item, loaded := u.Introducer.Get()
		fmt.Fprintf(&g.output, "Introducer loaded: %t\n", loaded)
		g.show(item)
	}
	g.introducerKey = false
	fmt.Fprintln(&g.output, "report-join-projection")
	g.show(must(reporting.SelectOrders(7).All(ctx, db)))
	fmt.Fprintln(&g.output, "select-value")
	g.show(must(ormrows.SelectValue(models.Orders.Query(), models.Orders.TotalCents).All(ctx, db)))
	fmt.Fprintln(&g.output, "soft-delete-read")
	g.show(must(reporting.Orders.Query().All(ctx, db)))
	fmt.Fprintln(&g.output, "soft-delete")
	id := must(model.ParseID[reporting.Order](orderID))
	g.show(must(reporting.Orders.Delete(ctx, db, id)))
	fmt.Fprintln(&g.output, "restore")
	g.show(must(reporting.Orders.Restore(ctx, db, id)))
	g.fixB(ctx, db)
}
