package scalarqueries_test

import (
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"testing"
)

func TestPostgresComputedRelationOrder(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		f := models.Users
		relation := models.UserLinks.Referrals.OrderBy(expr.RelationOrder(models.UserLinks.Referrals, expr.Negate(expr.Value(f.Age)).Asc()))
		rows, err := models.Users.Query().Where(f.ID.Eq(users[0].ID)).With(relation).All(t.Context(), tx)
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		children, _ := rows[0].Referrals.Get()
		if len(children) != 2 || children[0].ID != users[2].ID {
			t.Fatal("computed eager relation order", children)
		}
		return nil
	})
}

func TestPostgresComputedManyToManyOrdering(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		if _, err := tx.Exec(t.Context(), `CREATE TABLE friendships (id uuid PRIMARY KEY,from_id uuid NOT NULL,to_id uuid NOT NULL,note text NOT NULL)`); err != nil {
			return err
		}
		for i, note := range []string{"B", "a"} {
			if _, err := models.Friendships.Insert(t.Context(), tx, models.Friendships.FromID.Set(users[0].ID), models.Friendships.ToID.Set(users[i+1].ID), models.Friendships.Note.Set(note)); err != nil {
				return err
			}
		}
		u := models.Users
		p := models.Friendships
		for _, rel := range []orm.ThroughRelation[models.User, models.User, models.Friendship]{
			models.UserLinks.Friends.OrderBy(expr.RelationOrder(models.UserLinks.Friends, expr.Negate(expr.Value(u.Age)).Asc())),
			models.UserLinks.Friends.OrderByPivot(expr.RelationPivotOrder(models.UserLinks.Friends, expr.Lower(expr.Value(p.Note)).Asc())),
		} {
			rows, err := models.Users.Query().Where(u.ID.Eq(users[0].ID)).With(rel).All(t.Context(), tx)
			if err != nil || len(rows) != 1 {
				t.Fatal(rows, err)
			}
			links, loaded := rows[0].Friends.Get()
			if !loaded || len(links) != 2 || links[0].Model.ID != users[2].ID {
				t.Fatal("computed target/pivot qualification or order", links)
			}
		}
		return nil
	})
}
