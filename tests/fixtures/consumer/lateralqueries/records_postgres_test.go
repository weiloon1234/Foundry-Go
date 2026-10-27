package lateralqueries_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/lateralqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type usersAlias struct{}
type ordersAlias struct{}
type latestAlias struct{}
type statsAlias struct{}

func userOrders[L any](outer rows.ScopeSource[L], scope expr.Scope[L, models.User]) (rows.CorrelatedSource[L, rows.Alias[ordersAlias, models.Order]], expr.Scope[rows.Correlation[L, rows.Alias[ordersAlias, models.Order]], models.Order]) {
	orders := rows.As[ordersAlias](models.Orders.Query(), "orders_for_user")
	c := rows.Correlate(outer, orders)
	inner := rows.InnerScope(c, rows.Scope(orders))
	u, o := rows.OuterScope(c, scope), inner
	return c.Where(expr.Equal(expr.At(o, models.Orders.BuyerID), expr.At(u, models.Users.ID))), inner
}

func TestPostgresLateralPerParentRecords(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		outer := rows.As[usersAlias](models.Users.Query(), "buyer")
		c, scope := userOrders(outer, rows.Scope(outer))
		o := scope
		record := rows.SelectCorrelatedRecord(c, scope).OrderBy(expr.At(o, models.Orders.TotalCents).Desc(), expr.At(o, models.Orders.ID).Asc())
		if n, err := rows.ReadRecord(rows.SelectRecord(outer, rows.Scope(outer)).Where(record.Exists())).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("correlated complete-record existence", n, err)
		}
		latest := rows.AsLateral[latestAlias](record.Limit(1), "latest")
		joined := rows.LeftJoinLateral(outer, latest)
		u := rows.LeftScope(joined, rows.Scope(outer))
		r := rows.NullableRightScope(joined, rows.LateralScope(latest))
		results := rows.OrderBy(rows.Select(joined, lateralqueries.UserOrders, rows.MapSelected(lateralqueries.UserOrders.UserID, expr.Row(expr.At(u, models.Users.ID)).Value()), rows.MapSelected(lateralqueries.UserOrders.OrderID, expr.Row(expr.AtNullable(r, models.Orders.ID)).Value()), rows.MapSelected(lateralqueries.UserOrders.Total, expr.Row(expr.AtNullable(r, models.Orders.TotalCents)).Value())), expr.At(u, models.Users.Age).Asc())
		counter := &queryfixture.QueryCounter{Executor: tx}
		items, err := rows.Read(results).All(t.Context(), counter)
		if err != nil || len(items) != 4 || counter.Queries.Load() != 1 {
			t.Fatal(items, err, counter.Queries.Load())
		}
		for i, row := range items {
			if row.UserID != users[i].ID {
				t.Fatal(row)
			}
			total, present := row.Total.Get()
			if (i < 2 && (!present || total != int64(i+2))) || (i >= 2 && (present || !row.OrderID.IsNull())) {
				t.Fatal(row)
			}
		}
		// The full right record keeps its declared decoder and model-specific ID.
		crossed := rows.CrossJoinLateral(outer, latest)
		complete, err := rows.ReadRecord(rows.SelectRecord(crossed, rows.RightScope(crossed, rows.LateralScope(latest)))).All(t.Context(), tx)
		if err != nil || len(complete) != 2 {
			t.Fatal(complete, err)
		}
		for _, order := range complete {
			if order.TotalCents < 2 || order.BuyerID.IsZero() {
				t.Fatal(order)
			}
		}
		inner := rows.InnerJoinLateral(outer, latest)
		count, err := rows.ReadRecord(rows.SelectRecord(inner, rows.LeftScope(inner, rows.Scope(outer)))).Count(t.Context(), tx)
		if err != nil || count != 2 {
			t.Fatal(count, err)
		}
		// A per-parent offset is not a single offset over the completed join.
		second := rows.AsLateral[latestAlias](record.Limit(1).Offset(1), "second")
		secondJoin := rows.CrossJoinLateral(outer, second)
		secondRows, err := rows.ReadRecord(rows.SelectRecord(secondJoin, rows.RightScope(secondJoin, rows.LateralScope(second)))).All(t.Context(), tx)
		if err != nil || len(secondRows) != 1 || secondRows[0].TotalCents != 1 {
			t.Fatal(secondRows, err)
		}
		uf, of := rows.Scope(outer), rows.LateralScope(latest)
		on := rows.On(expr.At(uf, models.Users.ID), expr.At(of, models.Orders.BuyerID)).WhereRight(expr.OrderRow(expr.At(of, models.Orders.TotalCents)).Gt(2))
		filteredJoin := rows.LeftJoinLateral(outer, latest, on)
		filtered := rows.NullableRightScope(filteredJoin, rows.LateralScope(latest))
		values, err := rows.ReadScalar(rows.SelectScalar(filteredJoin, expr.Row(expr.AtNullable(filtered, models.Orders.ID)).Value())).All(t.Context(), tx)
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
		if count, err := rows.Read(results.Where(expr.OrderNullableRow(expr.AtNullable(r, models.Orders.TotalCents)).Gt(2))).Count(t.Context(), tx); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		// Existing result pagination, CTE/set composition and transaction locks retain the join.
		page, err := rows.Read(results).Paginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 2})
		if err != nil || len(page.Items) != 2 || page.Total != 4 {
			t.Fatal(page, err)
		}
		materialized := rows.AsRecord[statsAlias](rows.CTE("lateral_results", rows.RecordOf(results)).Materialized(), "cached")
		if count, err := rows.ReadRecord(rows.SelectRecord(materialized, rows.Scope(materialized))).Count(t.Context(), tx); err != nil || count != 4 {
			t.Fatal(count, err)
		}
		if count, err := rows.ReadSet(rows.UnionAll(rows.RecordOf(results), rows.RecordOf(results))).Count(t.Context(), tx); err != nil || count != 8 {
			t.Fatal(count, err)
		}
		if locked, err := rows.ForUpdate(rows.SelectRecord(joined, rows.LeftScope(joined, rows.Scope(outer)))).Of(rows.Target(rows.LeftScope(joined, rows.Scope(outer)))).All(t.Context(), tx); err != nil || len(locked) != 4 {
			t.Fatal(locked, err)
		}
		return nil
	})
}

func TestLateralInvalidContractsBeforeIO(t *testing.T) {
	outer := rows.As[usersAlias](models.Users.Query(), "buyer")
	c, scope := userOrders(outer, rows.Scope(outer))
	record := rows.SelectCorrelatedRecord(c, scope)
	for _, source := range []rows.LateralSource[latestAlias, rows.Alias[usersAlias, models.User], models.Order]{
		rows.AsLateral[latestAlias](record, "bad.name"),
		rows.AsLateral[latestAlias](record, "buyer"),
		rows.AsLateral[latestAlias](record.Limit(-1), "latest"),
		{},
	} {
		j := rows.LeftJoinLateral(outer, source)
		_, err := rows.ReadRecord(rows.SelectRecord(j, rows.LeftScope(j, rows.Scope(outer))).Limit(0)).All(t.Context(), queryfixture.NoQueries(t))
		if !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	latest := rows.AsLateral[latestAlias](record, "latest")
	j := rows.CrossJoinLateral(outer, latest)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rows.ReadRecord(rows.SelectRecord(j, rows.RightScope(j, rows.LateralScope(latest)))).All(ctx, queryfixture.NoQueries(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
