package lateralqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/lateralqueries"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type matchedAlias struct{}
type matchInputAlias struct{}

func TestPostgresLateralAggregateAndWindowReports(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, _ []models.User) error {
		outer := rows.As[usersAlias](models.Users.Query(), "buyer")
		c, scope := userOrders(outer, rows.Scope(outer))
		o := scope
		selected := rows.ProjectCorrelated(c, reports.OrderSummaries, rows.MapSelected(reports.OrderSummaries.Total, expr.SumAt(o, orm.NumberExpr(models.Orders.TotalCents)).Value()), rows.MapSelected(reports.OrderSummaries.Count, expr.CountFieldAt(o, orm.KeyExpr(models.Orders.ID)).Value()))
		stats := rows.AsLateral[statsAlias](selected, "stats")
		joined := rows.CrossJoinLateral(outer, stats)
		u := rows.LeftScope(joined, rows.Scope(outer))
		items, err := rows.ReadRecord(rows.SelectRecord(joined, rows.RightScope(joined, rows.LateralScope(stats))).OrderBy(expr.At(u, orm.NumberExpr(models.Users.Age)).Asc())).All(t.Context(), tx)
		if err != nil || len(items) != 4 {
			t.Fatal(items, err)
		}
		for i, want := range []int64{2, 1, 0, 0} {
			if items[i].Count != want {
				t.Fatal(items)
			}
			if total, present := items[i].Total.Get(); (i < 2 && (!present || total.String() != "3")) || (i >= 2 && present) {
				t.Fatal(items)
			}
		}
		// HAVING can remove the otherwise present aggregate row for an empty input.
		filtered := rows.AsLateral[statsAlias](rows.HavingCorrelatedRecord(selected, expr.CountFieldAt(o, orm.KeyExpr(models.Orders.ID)).Gt(0)), "nonempty")
		filteredJoin := rows.CrossJoinLateral(outer, filtered)
		if n, err := rows.ReadRecord(rows.SelectRecord(filteredJoin, rows.RightScope(filteredJoin, rows.LateralScope(filtered)))).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal(n, err)
		}
		window := expr.WindowFor(scope).OrderBy(expr.At(o, orm.NumberExpr(models.Orders.TotalCents)).Desc(), expr.At(o, orm.KeyExpr(models.Orders.ID)).Asc())
		ranked := rows.ProjectCorrelated(c, lateralqueries.RankedOrders, rows.MapSelected(lateralqueries.RankedOrders.ID, expr.Row(expr.At(o, orm.KeyExpr(models.Orders.ID))).Value()), rows.MapSelected(lateralqueries.RankedOrders.Total, expr.Row(expr.At(o, orm.NumberExpr(models.Orders.TotalCents))).Value()), rows.MapSelected(lateralqueries.RankedOrders.Rank, expr.RowNumber(window))).OrderBy(expr.At(o, orm.NumberExpr(models.Orders.TotalCents)).Desc()).Limit(2)
		ranking := rows.AsLateral[latestAlias](ranked, "ranked")
		rankingJoin := rows.CrossJoinLateral(outer, ranking)
		ranks, err := rows.ReadRecord(rows.SelectRecord(rankingJoin, rows.RightScope(rankingJoin, rows.LateralScope(ranking)))).All(t.Context(), tx)
		if err != nil || len(ranks) != 3 {
			t.Fatal(ranks, err)
		}
		for _, rank := range ranks {
			want := int64(1)
			if rank.Total == 1 {
				want = 2
			}
			if rank.Rank != want {
				t.Fatal(ranks)
			}
		}
		// A CTE referenced solely inside the lateral input is discovered once.
		definition := rows.CTE("eligible_orders", rows.ModelRecord(models.Orders.Query().Where(orm.NumberGt(models.Orders.TotalCents, 1))))
		eligible := rows.AsRecord[ordersAlias](definition, "eligible")
		cteCorrelation := rows.Correlate(outer, eligible)
		inside := rows.InnerScope(cteCorrelation, rows.Scope(eligible))
		e := inside
		buyer := rows.OuterScope(cteCorrelation, rows.Scope(outer))
		cteRecords := rows.DistinctCorrelatedRecord(rows.SelectCorrelatedRecord(cteCorrelation.Where(expr.Equal(expr.At(e, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(buyer, orm.KeyExpr(models.Users.ID)))), inside)).OrderBy(expr.At(e, orm.NumberExpr(models.Orders.TotalCents)).Desc()).Limit(1)
		cteRight := rows.AsLateral[latestAlias](cteRecords, "cte_latest")
		cteJoin := rows.CrossJoinLateral(outer, cteRight)
		if n, err := rows.ReadRecord(rows.SelectRecord(cteJoin, rows.RightScope(cteJoin, rows.LateralScope(cteRight)))).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal(n, err)
		}
		return nil
	})
}

func TestPostgresLateralChainedNullableScopes(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, _ []models.User) error {
		outer := rows.As[usersAlias](models.Users.Query(), "buyer")
		c, scope := userOrders(outer, rows.Scope(outer))
		o := scope
		latest := rows.AsLateral[latestAlias](rows.SelectCorrelatedRecord(c, scope).OrderBy(expr.At(o, orm.NumberExpr(models.Orders.TotalCents)).Desc()).Limit(1), "latest")
		first := rows.LeftJoinLateral(outer, latest)
		matches := rows.As[matchInputAlias](models.Users.Query(), "matched_user")
		second := rows.Correlate(first, matches)
		previous := rows.OuterNullableScope(second, rows.NullableRightScope(first, rows.LateralScope(latest)))
		matchScope := rows.InnerScope(second, rows.Scope(matches))
		m := matchScope
		matched := rows.AsLateral[matchedAlias](rows.SelectCorrelatedRecord(second.Where(expr.Equal(expr.NullableRow(expr.At(m, orm.KeyExpr(models.Users.ID))), expr.AtNullable(previous, orm.ColumnExpr(models.Orders.BuyerID)))), matchScope), "matched")
		chain := rows.LeftJoinLateral(first, matched)
		retained := rows.LeftNullableScope(chain, rows.NullableRightScope(first, rows.LateralScope(latest)))
		found := rows.NullableRightScope(chain, rows.LateralScope(matched))
		pairs, err := rows.Read(rows.Select(chain, reports.UserPairs, rows.MapSelected(reports.UserPairs.LeftID, expr.Row(expr.AtNullable(retained, orm.ColumnExpr(models.Orders.BuyerID))).Value()), rows.MapSelected(reports.UserPairs.RightID, expr.Row(expr.AtNullable(found, orm.KeyExpr(models.Users.ID))).Value()))).All(t.Context(), tx)
		if err != nil || len(pairs) != 4 {
			t.Fatal(pairs, err)
		}
		missing := 0
		for _, pair := range pairs {
			left, hasLeft := pair.LeftID.Get()
			right, hasRight := pair.RightID.Get()
			if hasLeft != hasRight || left != right {
				t.Fatal(pair)
			}
			if !hasLeft {
				missing++
			}
		}
		if missing != 2 {
			t.Fatal(pairs)
		}
		return nil
	})
}
