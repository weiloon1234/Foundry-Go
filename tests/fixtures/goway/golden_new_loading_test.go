package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"

	"foundry.test/goway/models"
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	ormrows "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (g newGolden) fixB(ctx context.Context, db *database.DB) {
	fmt.Fprintln(&g.output, "direct-join")
	orders, users := ormrows.As[orderSide](models.Orders.Query(), "o"), ormrows.As[memberSide](models.Users.Query(), "u")
	joined := ormrows.InnerJoin(orders, users, ormrows.On(expr.At(ormrows.Scope(orders), models.Orders.BuyerID), expr.At(ormrows.Scope(users), models.Users.ID)))
	type fields struct {
		ormrows.Row[EmailRow]
		Email ormrows.Field[EmailRow, string]
	}
	schema := ormrows.Define(func(b ormrows.Builder[EmailRow], r *EmailRow) fields {
		return fields{b.Row(), ormrows.TextOf(b, &r.Email)}
	})
	g.show(must(ormrows.Read(ormrows.Select(joined, schema, ormrows.Map(schema.Email, expr.At(ormrows.RightScope(joined, ormrows.Scope(users)), models.Users.Email)))).All(ctx, db)))
	fmt.Fprintln(&g.output, "load-duplicate-and-missing")
	buyer := must(model.ParseID[models.User](buyerID))
	missing := must(model.ParseID[models.User]("01890f47-1a00-7000-8000-000000000009"))
	g.emitOrders(must(orm.LoadFrom(ctx, db, models.Orders.Query().With(models.OrderRelations.Buyer), []models.Order{{BuyerID: buyer}, {BuyerID: buyer}, {BuyerID: missing}})))
	fmt.Fprintln(&g.output, "soft-target-relation")
	reports := must(reporting.Orders.Query().With(reporting.OrderRelations.Member).All(ctx, db))
	for _, o := range reports {
		g.show(o)
		v, loaded := o.Member.Get()
		fmt.Fprintf(&g.output, "Member loaded: %t\n", loaded)
		g.show(v)
	}
	fmt.Fprintln(&g.output, "with-first")
	first := must(models.Orders.Query().With(models.OrderRelations.Buyer).First(ctx, db))
	v, ok := first.Get()
	fmt.Fprintf(&g.output, "First present: %t\n", ok)
	g.emitOrders([]models.Order{v})
	fmt.Fprintln(&g.output, "with-paginate")
	page := must(models.Orders.Query().With(models.OrderRelations.Buyer).Paginate(ctx, db, orm.PageRequest{Number: 1, Size: 2}))
	fmt.Fprintf(&g.output, "Page: %d %d %d %d\n", page.Number, page.Size, page.Total, page.Pages)
	g.emitOrders(page.Items)
	fmt.Fprintln(&g.output, "with-chunk")
	g.chunkBatches = [][][]driver.Value{{{orderID, buyerID, int64(500)}}, {{"01890f47-1a00-7000-8000-000000000003", buyerID, int64(600)}}, {}}
	check(models.Orders.Query().With(models.OrderRelations.Buyer).Chunk(ctx, db, 1, func(items []models.Order) error { g.emitOrders(items); return nil }))
	g.chunkBatches = nil
}
func (g newGolden) emitOrders(items []models.Order) {
	g.show(items)
	for _, o := range items {
		v, loaded := o.Buyer.Get()
		fmt.Fprintf(&g.output, "Buyer loaded: %t\n", loaded)
		g.show(v)
	}
}
