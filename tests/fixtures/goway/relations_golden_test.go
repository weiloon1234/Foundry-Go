package goway_test

import (
	"context"
	"testing"

	"foundry.test/goway/models"
	"foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestDirectRelationSQLMatchesOldAPI(t *testing.T) {
	captures := []*goldenCapture{{introducerKey: true}, {introducerKey: true}}
	ctx := context.Background()
	for side, g := range captures {
		db := g.open(ctx)
		if side == 0 {
			uf, of := oldmodels.UserFields(), oldmodels.OrderFields()
			ur, or := oldmodels.UserRelations(), oldmodels.OrderRelations()
			orders, err := oldmodels.QueryOrders().With(or.Buyer.Where(uf.Age.Gt(18)).OrderBy(uf.Email.Desc()).With(ur.Introducer.Where(uf.Status.Eq(oldmodels.StatusActive)))).All(ctx, db)
			if err != nil || len(orders) != 1 {
				t.Fatal(err, len(orders))
			}
			buyer, _ := orders[0].Buyer.Get()
			m, present := buyer.Get()
			if !present || !m.Introducer.IsLoaded() {
				t.Fatal("nested belongs-to missing")
			}
			users, err := oldmodels.QueryUsers().With(ur.Orders.Where(of.TotalCents.Gt(100)).OrderBy(of.TotalCents.Desc()).With(or.Buyer)).All(ctx, db)
			if err != nil || len(users) != 1 || !users[0].Orders.IsLoaded() {
				t.Fatal(err)
			}
			for _, o := range users[0].Orders.All() {
				if !o.Buyer.IsLoaded() {
					t.Fatal("nested collection target missing")
				}
			}
			hasOne := query.HasOne(uf.ID, uf.IntroducerID).Bind("Introducer", oldmodels.QueryUsers().Query, oldmodels.QueryUsers().Query, func(m oldmodels.User) relation.One[oldmodels.User] { return m.Introducer }, func(m oldmodels.User, r relation.One[oldmodels.User]) oldmodels.User { m.Introducer = r; return m })
			loaded, err := oldmodels.QueryUsers().With(hasOne).Load(ctx, db, []oldmodels.User{{ID: must(model.ParseID[oldmodels.User](buyerID))}})
			if err != nil || !loaded[0].Introducer.IsLoaded() {
				t.Fatal(err)
			}
			constrained := ur.Orders.Where(or.Buyer.Where(uf.Age.Gt(18)).Exists())
			if _, err = oldmodels.QueryUsers().WhereHas(constrained).All(ctx, db); err != nil {
				t.Fatal(err)
			}
			if _, err = oldmodels.QueryUsers().WhereDoesntHave(constrained).All(ctx, db); err != nil {
				t.Fatal(err)
			}
		} else {
			uf, of := models.Users, models.Orders
			ur, or := models.UserRelations, models.OrderRelations
			orders, err := of.Query().With(or.Buyer.Where(uf.Age.Gt(18)).OrderBy(uf.Email.Desc()).With(ur.Introducer.Where(uf.Status.Eq(models.StatusActive)))).All(ctx, db)
			if err != nil || len(orders) != 1 {
				t.Fatal(err, len(orders))
			}
			buyer, _ := orders[0].Buyer.Get()
			m, present := buyer.Get()
			if !present || !m.Introducer.IsLoaded() {
				t.Fatal("nested belongs-to missing")
			}
			users, err := uf.Query().With(ur.Orders.Where(of.TotalCents.Gt(100)).OrderBy(of.TotalCents.Desc()).With(or.Buyer)).All(ctx, db)
			if err != nil || len(users) != 1 || !users[0].Orders.IsLoaded() {
				t.Fatal(err)
			}
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
			nested := orm.Has(or.Buyer, uf.Age.Gt(18))
			if _, err = uf.Query().Where(orm.Has(ur.Orders, nested)).All(ctx, db); err != nil {
				t.Fatal(err)
			}
			if _, err = uf.Query().Where(orm.DoesntHave(ur.Orders, nested)).All(ctx, db); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if captures[0].statements != 9 || captures[1].statements != 9 {
		t.Fatal("unexpected relation SQL count", captures[0].statements, captures[1].statements)
	}
	if captures[0].output.String() != captures[1].output.String() {
		t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
	}
}
