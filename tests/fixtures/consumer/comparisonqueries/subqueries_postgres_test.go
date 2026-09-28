package comparisonqueries_test

import (
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type thresholdAlias struct{}

func TestPostgresScalarRowsAndComparisonDiscovery(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		q := models.QueryUsers()
		f := models.UserFields()
		middle := query.ScalarRowQuery(q, query.SelectValue(q, f.Age.Value()).OrderBy(f.Age.Asc()).Offset(1).Limit(1))
		rows, err := q.Where(query.GreaterNullable(query.NullableRow(f.Age), middle)).All(t.Context(), tx)
		if err != nil || len(rows) != 1 || rows[0].ID != users[2].ID {
			t.Fatal(rows, err)
		}
		count := query.Count[models.User]()
		total := query.ScalarRowQuery(q, query.SelectValue(q, count.Value()))
		if n, err := q.Where(query.Equal(total, query.NullableRow(count.Param(3)))).Count(t.Context(), tx); err != nil || n != 3 {
			t.Fatal("inner aggregate leaked into outer row phase", n, err)
		}
		cte := query.As[thresholdAlias](query.CTE("adult_threshold", q.Where(f.Age.Gt(20))), "threshold")
		cf := models.UserFieldsAt(cte.Scope())
		threshold := query.ScalarRowQuery(q, query.SelectValue(cte, cf.Age.Value()).OrderBy(cf.Age.Asc()).Limit(1))
		selected := q.Where(query.Equal(f.Age, query.Coalesce(threshold, f.Age.Param(-1))))
		statement, err := selected.Compile()
		if err != nil || !strings.HasPrefix(statement.SQL(), "WITH ") {
			t.Fatal("RHS scalar CTE was not discovered", statement.SQL(), err)
		}
		rows, err = selected.All(t.Context(), tx)
		if err != nil || len(rows) != 1 || rows[0].ID != users[1].ID {
			t.Fatal(rows, err)
		}
		empty := query.ScalarRowQuery(q, query.SelectValue(q.Limit(0), f.Age.Value()))
		if n, err := q.Where(query.Equal(query.NullableRow(f.Age), empty)).Count(t.Context(), tx); err != nil || n != 0 {
			t.Fatal("empty scalar lost NULL", n, err)
		}
		// No implicit limit may hide invalid scalar cardinality.
		err = tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			_, err := q.Where(query.Equal(query.NullableRow(f.Age), query.ScalarRowQuery(q, query.SelectValue(q, f.Age.Value())))).All(t.Context(), inner)
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
		q := models.QueryUsers()
		f := models.UserFields()
		orders := models.QueryOrders()
		link := query.Correlate(q, orders)
		buyer := models.UserFieldsAt(query.OuterScope(link, q.Scope()))
		order := models.OrderFieldsAt(query.InnerScope(link, orders.Scope()))
		matching := link.Where(query.Equal(order.BuyerID, buyer.ID))
		countQuery := query.SelectCorrelatedValue(matching, order.ID.Count().Value())
		orderCount := query.CorrelatedScalarRowQuery(countQuery)
		zero := query.NullableRow(query.Count[models.User]().Param(0))
		purchased := query.GreaterNullable(orderCount, zero)
		rows, err := q.Where(purchased).OrderBy(f.Age.Asc()).All(t.Context(), tx)
		if err != nil || len(rows) != 2 || rows[0].ID != users[0].ID || rows[1].ID != users[1].ID {
			t.Fatal(rows, err)
		}
		total := query.CorrelatedScalarNullableRowQuery(query.SelectCorrelatedValue(matching, order.TotalCents.Sum().Value()))
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
