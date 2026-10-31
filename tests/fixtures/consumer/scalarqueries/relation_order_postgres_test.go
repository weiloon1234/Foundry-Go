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
		relation := models.UserLinks.Referrals.OrderBy(expr.RelationOrder(models.UserLinks.Referrals, expr.Asc(expr.Negate(expr.Value(orm.NumberExpr(f.Age))))))
		rows, err := orm.All(t.Context(), tx, models.Users.Query().Where(orm.KeyEq(f.ID, users[0].ID)).With(relation))
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
			if _, err := orm.Insert(t.Context(), tx, models.Friendships.Table, orm.ColumnSet(models.Friendships.FromID, users[0].ID), orm.ColumnSet(models.Friendships.ToID, users[i+1].ID), orm.TextSet(models.Friendships.Note, note)); err != nil {
				return err
			}
		}
		u := models.Users
		p := models.Friendships
		for _, rel := range []orm.ThroughRelation[models.User, models.User, models.Friendship]{
			models.UserLinks.Friends.OrderBy(expr.RelationOrder(models.UserLinks.Friends, expr.Asc(expr.Negate(expr.Value(orm.NumberExpr(u.Age)))))),
			models.UserLinks.Friends.OrderByPivot(expr.RelationPivotOrder(models.UserLinks.Friends, expr.Asc(expr.Lower(expr.Value(orm.TextExpr(p.Note)))))),
		} {
			rows, err := orm.All(t.Context(), tx, models.Users.Query().Where(orm.KeyEq(u.ID, users[0].ID)).With(rel))
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
