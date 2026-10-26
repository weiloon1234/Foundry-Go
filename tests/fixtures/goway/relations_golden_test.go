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
	orders, err := of.Query().With(or.Buyer.Where(uf.Age.Gt(18)).OrderBy(uf.Email.Desc()).With(ur.Introducer.Where(uf.Status.Eq(models.StatusActive)))).All(ctx, db)
	if err != nil || len(orders) != 1 {
		t.Fatal(err, len(orders))
	}
	g.showHydrated(orders)
	buyer, _ := orders[0].Buyer.Get()
	m, present := buyer.Get()
	if !present || !m.Introducer.IsLoaded() {
		t.Fatal("nested belongs-to missing")
	}
	users, err := uf.Query().With(ur.Orders.Where(of.TotalCents.Gt(100)).OrderBy(of.TotalCents.Desc()).With(or.Buyer)).All(ctx, db)
	if err != nil || len(users) != 1 || !users[0].Orders.IsLoaded() {
		t.Fatal(err)
	}
	g.showHydrated(users)
	for _, o := range users[0].Orders.All() {
		if !o.Buyer.IsLoaded() {
			t.Fatal("nested collection target missing")
		}
	}
	hasOne := orm.HasOneOf(uf.Introducer, uf.ID, uf.IntroducerID)
	loaded, err := orm.Load(ctx, db, []models.User{{ID: must(model.ParseID[models.User](buyerID))}}, hasOne)
	if err != nil || !loaded[0].Introducer.IsLoaded() {
		t.Fatal(err)
	}
	g.showHydrated(loaded)
	nested := orm.Has(or.Buyer, uf.Age.Gt(18))
	if _, err = uf.Query().Where(orm.Has(ur.Orders, nested)).All(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = uf.Query().Where(orm.DoesntHave(ur.Orders, nested)).All(ctx, db); err != nil {
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
