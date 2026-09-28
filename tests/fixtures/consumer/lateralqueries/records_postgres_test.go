package lateralqueries_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/lateralqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type usersAlias struct{}
type ordersAlias struct{}
type latestAlias struct{}
type statsAlias struct{}

func userOrders[L any](outer query.ScopeSource[L], scope query.RecordScope[L, models.User]) (query.CorrelatedSource[L, query.Alias[ordersAlias, models.Order]], query.RecordScope[query.Correlation[L, query.Alias[ordersAlias, models.Order]], models.Order]) {
	orders := query.As[ordersAlias](models.QueryOrders(), "orders_for_user")
	c := query.Correlate(outer, orders)
	inner := query.InnerScope(c, orders.Scope())
	u, o := models.UserFieldsAt(query.OuterScope(c, scope)), models.OrderFieldsAt(inner)
	return c.Where(query.Equal(o.BuyerID, u.ID)), inner
}

func TestPostgresLateralPerParentRecords(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		outer := query.As[usersAlias](models.QueryUsers(), "buyer")
		c, scope := userOrders(outer, outer.Scope())
		o := models.OrderFieldsAt(scope)
		record := query.SelectCorrelatedRecord(c, scope).OrderBy(o.TotalCents.Desc(), o.ID.Asc())
		if n, err := query.SelectRecord(outer, outer.Scope()).Where(record.Exists()).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("correlated complete-record existence", n, err)
		}
		latest := query.AsLateral[latestAlias](record.Limit(1), "latest")
		joined := query.LeftJoinLateral(outer, latest)
		u := models.UserFieldsAt(query.LeftScope(joined, outer.Scope()))
		r := models.OrderNullableFieldsAt(query.NullableRightScope(joined, latest.Scope()))
		results := lateralqueries.ProjectUserOrder(joined).SelectUserID(u.ID.Value()).SelectOrderID(r.ID.Value()).SelectTotal(r.TotalCents.Value()).Query().OrderBy(u.Age.Asc())
		counter := &queryfixture.QueryCounter{Executor: tx}
		rows, err := results.All(t.Context(), counter)
		if err != nil || len(rows) != 4 || counter.Queries.Load() != 1 {
			t.Fatal(rows, err, counter.Queries.Load())
		}
		for i, row := range rows {
			if row.UserID != users[i].ID {
				t.Fatal(row)
			}
			total, present := row.Total.Get()
			if (i < 2 && (!present || total != int64(i+2))) || (i >= 2 && (present || !row.OrderID.IsNull())) {
				t.Fatal(row)
			}
		}
		// The full right record keeps its generated decoder and model-specific ID.
		crossed := query.CrossJoinLateral(outer, latest)
		complete, err := query.SelectRecord(crossed, query.RightScope(crossed, latest.Scope())).All(t.Context(), tx)
		if err != nil || len(complete) != 2 {
			t.Fatal(complete, err)
		}
		for _, order := range complete {
			if order.TotalCents < 2 || order.BuyerID.IsZero() {
				t.Fatal(order)
			}
		}
		inner := query.InnerJoinLateral(outer, latest)
		count, err := query.SelectRecord(inner, query.LeftScope(inner, outer.Scope())).Count(t.Context(), tx)
		if err != nil || count != 2 {
			t.Fatal(count, err)
		}
		// A per-parent offset is not a single offset over the completed join.
		second := query.AsLateral[latestAlias](record.Limit(1).Offset(1), "second")
		secondJoin := query.CrossJoinLateral(outer, second)
		secondRows, err := query.SelectRecord(secondJoin, query.RightScope(secondJoin, second.Scope())).All(t.Context(), tx)
		if err != nil || len(secondRows) != 1 || secondRows[0].TotalCents != 1 {
			t.Fatal(secondRows, err)
		}
		uf, of := models.UserFieldsAt(outer.Scope()), models.OrderFieldsAt(latest.Scope())
		on := query.On(uf.ID, of.BuyerID).WhereRight(of.TotalCents.Gt(2))
		filteredJoin := query.LeftJoinLateral(outer, latest, on)
		filtered := models.OrderNullableFieldsAt(query.NullableRightScope(filteredJoin, latest.Scope()))
		values, err := query.SelectValue(filteredJoin, filtered.ID.Value()).All(t.Context(), tx)
		if err != nil || len(values) != 4 {
			t.Fatal(values, err)
		}
		nonNull := 0
		for _, v := range values {
			if !v.IsNull() {
				nonNull++
			}
		}
		if nonNull != 1 {
			t.Fatal(values)
		}
		if count, err := results.Where(r.TotalCents.Gt(2)).Count(t.Context(), tx); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		// Existing result pagination, CTE/set composition and transaction locks retain the join.
		page, err := results.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 2})
		if err != nil || len(page.Items) != 2 || page.Total != 4 {
			t.Fatal(page, err)
		}
		materialized := query.As[statsAlias](query.CTE("lateral_results", results).Materialized(), "cached")
		if count, err := query.SelectRecord(materialized, materialized.Scope()).Count(t.Context(), tx); err != nil || count != 4 {
			t.Fatal(count, err)
		}
		if count, err := results.UnionAll(results).Count(t.Context(), tx); err != nil || count != 8 {
			t.Fatal(count, err)
		}
		if locked, err := query.SelectRecord(joined, query.LeftScope(joined, outer.Scope())).ForUpdate().Of(query.LeftScope(joined, outer.Scope())).All(t.Context(), tx); err != nil || len(locked) != 4 {
			t.Fatal(locked, err)
		}
		return nil
	})
}

func TestLateralInvalidContractsBeforeIO(t *testing.T) {
	outer := query.As[usersAlias](models.QueryUsers(), "buyer")
	c, scope := userOrders(outer, outer.Scope())
	record := query.SelectCorrelatedRecord(c, scope)
	for _, source := range []query.LateralSource[latestAlias, query.Alias[usersAlias, models.User], models.Order]{
		query.AsLateral[latestAlias](record, "bad.name"),
		query.AsLateral[latestAlias](record, "buyer"),
		query.AsLateral[latestAlias](record.Limit(-1), "latest"),
		{},
	} {
		j := query.LeftJoinLateral(outer, source)
		_, err := query.SelectRecord(j, query.LeftScope(j, outer.Scope())).Limit(0).All(t.Context(), queryfixture.NoQueries(t))
		if !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	latest := query.AsLateral[latestAlias](record, "latest")
	j := query.CrossJoinLateral(outer, latest)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := query.SelectRecord(j, query.RightScope(j, latest.Scope())).All(ctx, queryfixture.NoQueries(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
