package goway_test

import (
	"testing"

	"foundry.test/goway/models"
	"foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestLoadFromQuerySQLMatchesLegacy(t *testing.T) {
	captures := []*goldenCapture{{}, {}}
	for side, g := range captures {
		db := g.open(t.Context())
		if side == 0 {
			rel := oldmodels.UserRelations().Orders.OrderBy(oldmodels.OrderFields().TotalCents.Desc()).With(oldmodels.OrderRelations().Buyer)
			q := oldmodels.QueryUsers().Where(oldmodels.UserFields().Age.Gt(999)).Limit(0).With(rel).WithRelationLimits(query.RelationLimits{BatchSize: 1, MaxRows: 10, MaxDepth: 2})
			parents := []oldmodels.User{{ID: must(model.ParseID[oldmodels.User](buyerID))}}
			loaded, err := q.Load(t.Context(), db, parents)
			if err != nil || len(loaded) != 1 || loaded[0].Orders.Len() != 1 || parents[0].Orders.IsLoaded() {
				t.Fatal(loaded, err)
			}
			if _, err = q.LoadMissing(t.Context(), db, loaded); err != nil {
				t.Fatal(err)
			}
			if _, err = q.LoadMissing(t.Context(), db, parents); err != nil {
				t.Fatal(err)
			}
		} else {
			rel := models.UserRelations.Orders.OrderBy(models.Orders.TotalCents.Desc()).With(models.OrderRelations.Buyer)
			q := orm.WithRelationLimits(models.Users.Query().Where(models.Users.Age.Gt(999)).Limit(0).With(rel), orm.RelationLimits{BatchSize: 1, MaxRows: 10, MaxDepth: 2})
			parents := []models.User{{ID: must(model.ParseID[models.User](buyerID))}}
			loaded, err := orm.LoadFrom(t.Context(), db, q, parents)
			if err != nil || len(loaded) != 1 || loaded[0].Orders.Len() != 1 || parents[0].Orders.IsLoaded() {
				t.Fatal(loaded, err)
			}
			if _, err = orm.LoadMissingFrom(t.Context(), db, q, loaded); err != nil {
				t.Fatal(err)
			}
			if _, err = orm.LoadMissingFrom(t.Context(), db, q, parents); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		if g.statements != 4 {
			t.Fatal("expected two nested loads and no parent statement", g.statements)
		}
	}
	if captures[0].output.String() != captures[1].output.String() {
		t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
	}
}
