package goway_test

import (
	"context"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm"

	"github.com/weiloon1234/Foundry-Go/model"
)

func TestDirectRelationSQLMatchesOldAPI(t *testing.T) {
	g := &goldenCapture{introducerKey: true}
	ctx := context.Background()
	db := g.open(ctx)

	uf, of := models.Users, models.Orders
	ur, or := models.UserRelations, models.OrderRelations
	orders, err := orm.All(ctx, db, of.Query().With(or.Buyer.Where(orm.NumberGt(uf.Age, 18)).OrderBy(orm.TextDesc(uf.Email)).With(ur.Introducer.Where(orm.EnumEq(uf.Status, models.StatusActive)))))
	if err != nil || len(orders) != 1 {
		t.Fatal(err, len(orders))
	}
	g.showHydrated(orders)
	buyer, _ := orders[0].Buyer.Get()
	m, present := buyer.Get()
	if !present || !m.Introducer.IsLoaded() {
		t.Fatal("nested belongs-to missing")
	}
	users, err := orm.All(ctx, db, uf.Query().With(ur.Orders.Where(orm.NumberGt(of.TotalCents, 100)).OrderBy(orm.NumberDesc(of.TotalCents)).With(or.Buyer)))
	if err != nil || len(users) != 1 || !users[0].Orders.IsLoaded() {
		t.Fatal(err)
	}
	g.showHydrated(users)
	for _, o := range users[0].Orders.All() {
		if !o.Buyer.IsLoaded() {
			t.Fatal("nested collection target missing")
		}
	}
	hasOne := orm.HasOneOf(uf.Introducer, orm.KeyExpr(uf.ID), orm.NullableColumnExpr(uf.IntroducerID))
	loaded, err := orm.LoadFrom(ctx, db, models.Users.Query().With(hasOne), []models.User{{ID: must(model.ParseID[models.User](buyerID))}})
	if err != nil || !loaded[0].Introducer.IsLoaded() {
		t.Fatal(err)
	}
	g.showHydrated(loaded)
	nested := orm.Has(or.Buyer, orm.NumberGt(uf.Age, 18))
	if _, err = orm.All(ctx, db, uf.Query().Where(orm.Has(ur.Orders, nested))); err != nil {
		t.Fatal(err)
	}
	if _, err = orm.All(ctx, db, uf.Query().Where(orm.Not(orm.Has(ur.Orders, nested)))); err != nil {
		t.Fatal(err)
	}

	if err := db.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if g.statements != 9 {
		t.Fatal("unexpected relation SQL count", g.statements)
	}
	if !oracleEqual(t, "relations_golden/1", g.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}
