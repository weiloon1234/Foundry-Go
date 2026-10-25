package comparisonqueries_test

import (
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type thresholdAlias struct{}

func TestPostgresScalarRowsAndComparisonDiscovery(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		q := models.Users.Query()

		middle := rows.ScalarRowQuery(rows.From(q), rows.LimitScalar(rows.OffsetScalar(rows.OrderScalar(rows.SelectScalar(rows.From(q), expr.Select(expr.Value(models.Users.Age))), expr.Value(models.Users.Age).Asc()), 1), 1))
		items, err := expr.Where(q, expr.GreaterNullable(expr.NullableRow(expr.Value(models.Users.Age)), middle)).All(t.Context(), tx)
		if err != nil || len(items) != 1 || items[0].ID != users[2].ID {
			t.Fatal(items, err)
		}
		count := expr.Count[models.User]()
		total := rows.ScalarRowQuery(rows.From(q), rows.SelectScalar(rows.From(q), count.Value()))
		if n, err := expr.Where(q, expr.Equal(total, expr.NullableRow(count.Param(3)))).Count(t.Context(), tx); err != nil || n != 3 {
			t.Fatal("inner aggregate leaked into outer row phase", n, err)
		}
		cte := rows.AsRecord[thresholdAlias](rows.CTE("adult_threshold", rows.ModelRecord(q.Where(models.Users.Age.Gt(20)))), "threshold")
		cf := rows.Scope(cte)
		threshold := rows.ScalarRowQuery(rows.From(q), rows.LimitScalar(rows.OrderScalar(rows.SelectScalar(cte, expr.Select(expr.At(cf, models.Users.Age))), expr.At(cf, models.Users.Age).Asc()), 1))
		selected := expr.Where(q, expr.Equal(expr.Value(models.Users.Age), expr.Coalesce(threshold, expr.Param(models.Users.Age, -1))))
		statement, err := orm.Compile(selected)
		if err != nil || !strings.HasPrefix(statement.SQL(), "WITH ") {
			t.Fatal("RHS scalar CTE was not discovered", statement.SQL(), err)
		}
		items, err = selected.All(t.Context(), tx)
		if err != nil || len(items) != 1 || items[0].ID != users[1].ID {
			t.Fatal(items, err)
		}
		empty := rows.ScalarRowQuery(rows.From(q), rows.SelectScalar(rows.From(q.Limit(0)), expr.Select(expr.Value(models.Users.Age))))
		if n, err := expr.Where(q, expr.Equal(expr.NullableRow(expr.Value(models.Users.Age)), empty)).Count(t.Context(), tx); err != nil || n != 0 {
			t.Fatal("empty scalar lost NULL", n, err)
		}
		// No implicit limit may hide invalid scalar cardinality.
		err = tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			_, err := expr.Where(q, expr.Equal(expr.NullableRow(expr.Value(models.Users.Age)), rows.ScalarRowQuery(rows.From(q), rows.SelectScalar(rows.From(q), expr.Select(expr.Value(models.Users.Age)))))).All(t.Context(), inner)
			return err
		})
		var dbError *database.Error
		if !errors.As(err, &dbError) || dbError.SQLState() != "21000" {
			t.Fatal("scalar cardinality was hidden", err)
		}
		return nil
	})
}

func TestPostgresCorrelatedRowComparisonsAndRelationAliases(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		q := models.Users.Query()
		f := models.Users
		orders := models.Orders.Query()
		link := rows.Correlate(rows.From(q), rows.From(orders))
		buyer := rows.OuterScope(link, rows.ModelScope(q))
		order := rows.InnerScope(link, rows.ModelScope(orders))
		matching := link.Where(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(buyer, models.Users.ID)))
		countQuery := rows.SelectCorrelatedScalar(matching, expr.CountFieldAt(order, models.Orders.ID).Value())
		orderCount := rows.CorrelatedScalarRowQuery(countQuery)
		zero := expr.NullableRow(expr.Count[models.User]().Param(0))
		purchased := expr.GreaterNullable(orderCount, zero)
		items, err := expr.Where(q, purchased).OrderBy(f.Age.Asc()).All(t.Context(), tx)
		if err != nil || len(items) != 2 || items[0].ID != users[0].ID || items[1].ID != users[1].ID {
			t.Fatal(items, err)
		}
		total := rows.CorrelatedScalarNullableRowQuery(rows.SelectCorrelatedScalar(matching, expr.SumAt(order, models.Orders.TotalCents).Value()))
		if n, err := expr.Where(q, expr.NotDistinctFrom(total, expr.NullFor(expr.DecimalOf(expr.Value(f.Age))))).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("nullable correlated total lost NULL", n, err)
		}
		items, err = q.Where(orm.Has(models.UserLinks.Referrals.Where(expr.RelationFilter(models.UserLinks.Referrals, purchased)))).All(t.Context(), tx)
		if err != nil || len(items) != 1 || items[0].ID != users[0].ID {
			t.Fatal("direct relation comparison alias", items, err)
		}
		if err := queryfixture.CreateFriendshipTable(t.Context(), tx); err != nil {
			return err
		}
		for _, target := range users[1:3] {
			if _, err := models.Friendships.Insert(t.Context(), tx, models.Friendships.FromID.Set(users[0].ID), models.Friendships.ToID.Set(target.ID), models.Friendships.Note.Set("linked")); err != nil {
				return err
			}
		}
		items, err = q.Where(f.ID.Eq(users[0].ID)).With(models.UserLinks.Friends.Where(expr.RelationFilter(models.UserLinks.Friends, purchased))).All(t.Context(), tx)
		if err != nil || len(items) != 1 {
			t.Fatal(items, err)
		}
		links, loaded := items[0].Friends.Get()
		if !loaded || len(links) != 1 || links[0].Model.ID != users[1].ID {
			t.Fatal("through relation scalar comparison alias", links)
		}
		// Reusing the original condition must still address its original model.
		if n, err := expr.Where(q, purchased).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("relation qualification mutated original predicate", n, err)
		}
		return nil
	})
}
