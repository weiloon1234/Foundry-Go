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
	"github.com/weiloon1234/Foundry-Go/database/query"
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

// TODO(stage9-lane-a): migrate this test after expression predicates can enter relation filters.
func TestPostgresCorrelatedRowComparisonsAndRelationAliases(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		q := models.QueryUsers()
		f := models.UserFields()
		orders := models.QueryOrders()
		link := query.Correlate(q, orders)
		buyer := models.UserFieldsAt(query.OuterScope(link, q.Scope()))
		order := models.OrderFieldsAt(query.InnerScope(link, orders.Scope()))
		matching := link.Where(query.Equal(order.BuyerID, buyer.ID))
		countQuery := query.SelectCorrelatedValue(matching, query.CountField(order.ID).Value())
		orderCount := query.CorrelatedScalarRowQuery(countQuery)
		zero := query.NullableRow(query.Count[models.User]().Param(0))
		purchased := query.GreaterNullable(orderCount, zero)
		rows, err := q.Where(purchased).OrderBy(f.Age.Asc()).All(t.Context(), tx)
		if err != nil || len(rows) != 2 || rows[0].ID != users[0].ID || rows[1].ID != users[1].ID {
			t.Fatal(rows, err)
		}
		total := query.CorrelatedScalarNullableRowQuery(query.SelectCorrelatedValue(matching, query.Sum(order.TotalCents).Value()))
		if n, err := q.Where(query.NotDistinctFrom(total, query.NullFor(query.DecimalOf(f.Age)))).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("nullable correlated total lost NULL", n, err)
		}
		rows, err = q.WhereHas(models.UserRelations().Referrals.Where(purchased)).All(t.Context(), tx)
		if err != nil || len(rows) != 1 || rows[0].ID != users[0].ID {
			t.Fatal("direct relation comparison alias", rows, err)
		}
		if err := queryfixture.CreateFriendshipTable(t.Context(), tx); err != nil {
			return err
		}
		for _, target := range users[1:3] {
			if _, err := models.QueryFriendships().Create(t.Context(), tx, models.FriendshipDraft{}.SetFromID(users[0].ID).SetToID(target.ID).SetNote("linked")); err != nil {
				return err
			}
		}
		rows, err = q.Where(f.ID.Eq(users[0].ID)).With(models.UserRelations().Friends.Where(purchased)).All(t.Context(), tx)
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		links, loaded := rows[0].Friends.Get()
		if !loaded || len(links) != 1 || links[0].Model.ID != users[1].ID {
			t.Fatal("through relation scalar comparison alias", links)
		}
		// Reusing the original condition must still address its original model.
		if n, err := q.Where(purchased).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("relation qualification mutated original predicate", n, err)
		}
		return nil
	})
}
