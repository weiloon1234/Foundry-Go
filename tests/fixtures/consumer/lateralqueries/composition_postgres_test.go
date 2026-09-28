package lateralqueries_test

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/lateralqueries"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type matchedAlias struct{}
type matchInputAlias struct{}

func TestPostgresLateralAggregateAndWindowReports(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, _ []models.User) error {
		outer := query.As[usersAlias](models.QueryUsers(), "buyer")
		c, scope := userOrders(outer, outer.Scope())
		o := models.OrderFieldsAt(scope)
		selected := reports.ProjectCorrelatedOrderSummary(c).
			SelectTotal(o.TotalCents.Sum().Value()).SelectCount(o.ID.Count().Value()).Query()
		stats := query.AsLateral[statsAlias](selected, "stats")
		joined := query.CrossJoinLateral(outer, stats)
		u := models.UserFieldsAt(query.LeftScope(joined, outer.Scope()))
		rows, err := query.SelectRecord(joined, query.RightScope(joined, stats.Scope())).OrderBy(u.Age.Asc()).All(t.Context(), tx)
		if err != nil || len(rows) != 4 {
			t.Fatal(rows, err)
		}
		for i, want := range []int64{2, 1, 0, 0} {
			if rows[i].Count != want {
				t.Fatal(rows)
			}
			if total, present := rows[i].Total.Get(); (i < 2 && (!present || total.String() != "3")) || (i >= 2 && present) {
				t.Fatal(rows)
			}
		}
		// HAVING can remove the otherwise present aggregate row for an empty input.
		filtered := query.AsLateral[statsAlias](selected.Having(o.ID.Count().Gt(0)), "nonempty")
		filteredJoin := query.CrossJoinLateral(outer, filtered)
		if n, err := query.SelectRecord(filteredJoin, query.RightScope(filteredJoin, filtered.Scope())).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal(n, err)
		}
		window := query.WindowFor(c).OrderBy(o.TotalCents.Desc(), o.ID.Asc())
		ranked := lateralqueries.ProjectCorrelatedRankedOrder(c).SelectID(o.ID.Value()).SelectTotal(o.TotalCents.Value()).SelectRank(query.RowNumber(window)).Query().OrderBy(o.TotalCents.Desc()).Limit(2)
		ranking := query.AsLateral[latestAlias](ranked, "ranked")
		rankingJoin := query.CrossJoinLateral(outer, ranking)
		ranks, err := query.SelectRecord(rankingJoin, query.RightScope(rankingJoin, ranking.Scope())).All(t.Context(), tx)
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
		definition := query.CTE("eligible_orders", models.QueryOrders().Where(models.OrderFields().TotalCents.Gt(1)))
		eligible := query.As[ordersAlias](definition, "eligible")
		cteCorrelation := query.Correlate(outer, eligible)
		inside := query.InnerScope(cteCorrelation, eligible.Scope())
		e := models.OrderFieldsAt(inside)
		buyer := models.UserFieldsAt(query.OuterScope(cteCorrelation, outer.Scope()))
		cteRecords := query.SelectCorrelatedRecord(cteCorrelation.Where(query.Equal(e.BuyerID, buyer.ID)), inside).Distinct().OrderBy(e.TotalCents.Desc()).Limit(1)
		cteRight := query.AsLateral[latestAlias](cteRecords, "cte_latest")
		cteJoin := query.CrossJoinLateral(outer, cteRight)
		if n, err := query.SelectRecord(cteJoin, query.RightScope(cteJoin, cteRight.Scope())).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal(n, err)
		}
		return nil
	})
}

func TestPostgresLateralChainedNullableScopes(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, _ []models.User) error {
		outer := query.As[usersAlias](models.QueryUsers(), "buyer")
		c, scope := userOrders(outer, outer.Scope())
		o := models.OrderFieldsAt(scope)
		latest := query.AsLateral[latestAlias](query.SelectCorrelatedRecord(c, scope).OrderBy(o.TotalCents.Desc()).Limit(1), "latest")
		first := query.LeftJoinLateral(outer, latest)
		matches := query.As[matchInputAlias](models.QueryUsers(), "matched_user")
		second := query.Correlate(first, matches)
		previous := models.OrderNullableFieldsAt(query.OuterNullableScope(second, query.NullableRightScope(first, latest.Scope())))
		matchScope := query.InnerScope(second, matches.Scope())
		m := models.UserFieldsAt(matchScope)
		matched := query.AsLateral[matchedAlias](query.SelectCorrelatedRecord(second.Where(query.Equal(query.NullableRow(m.ID), previous.BuyerID)), matchScope), "matched")
		chain := query.LeftJoinLateral(first, matched)
		retained := models.OrderNullableFieldsAt(query.LeftNullableScope(chain, query.NullableRightScope(first, latest.Scope())))
		found := models.UserNullableFieldsAt(query.NullableRightScope(chain, matched.Scope()))
		pairs, err := reports.ProjectUserPair(chain).SelectLeftID(retained.BuyerID.Value()).SelectRightID(found.ID.Value()).Query().All(t.Context(), tx)
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
