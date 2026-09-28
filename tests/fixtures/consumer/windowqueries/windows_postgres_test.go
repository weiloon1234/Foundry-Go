package windowqueries_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rankedAlias struct{}
type orderAlias struct{}
type parentAlias struct{}
type childAlias struct{}

func runWindows(t *testing.T, check func(*database.Tx, []models.User) error) {
	t.Helper()
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		// First buyer has ordered amounts 1, 2, 2, 3; second buyer has 3.
		for _, amount := range []int64{2, 3} {
			if _, err := models.QueryOrders().Create(t.Context(), tx, models.OrderDraft{}.SetBuyerID(users[0].ID).SetTotalCents(amount)); err != nil {
				return err
			}
		}
		return check(tx, users)
	})
}

func ranking() query.ProjectionQuery[models.Order, windowqueries.RankingRow] {
	base := models.QueryOrders()
	o := models.OrderFields()
	peers := query.WindowFor(base).PartitionBy(o.BuyerID.Group()).OrderBy(o.TotalCents.Asc())
	ordered := peers.OrderBy(o.ID.Asc())
	return windowqueries.SelectRankingRow(base, windowqueries.RankingRowSelection[models.Order]{
		ID: o.ID.Value(), BuyerID: o.BuyerID.Value(), Number: query.RowNumber(ordered),
		Rank: query.Rank(peers), Dense: query.DenseRank(peers), Percent: query.PercentRank(peers),
		Cumulative: query.CumeDist(peers), Bucket: query.NTile(2, ordered),
	}).OrderBy(o.BuyerID.Asc(), o.TotalCents.Asc(), o.ID.Asc())
}

func TestPostgresWindowRankingAndComposition(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		q := ranking()
		rows, err := q.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(rows) != 5 {
			t.Fatal("ranking collapsed rows into groups")
		}
		var first []windowqueries.RankingRow
		for _, row := range rows {
			if row.BuyerID == users[0].ID {
				first = append(first, row)
			} else if row.Number != 1 || row.Rank != 1 || row.Percent != 0 || row.Cumulative != 1 {
				t.Fatal("single-row partition rank incorrect")
			}
		}
		for i, row := range first {
			if row.Number != int64(i+1) || row.Rank != []int64{1, 2, 2, 4}[i] || row.Dense != []int64{1, 2, 2, 3}[i] || row.Bucket != []int32{1, 1, 2, 2}[i] || math.Abs(row.Percent-[]float64{0, 1.0 / 3, 1.0 / 3, 1}[i]) > 1e-12 || row.Cumulative != []float64{0.25, 0.75, 0.75, 1}[i] {
				t.Fatal("peer ranking or codec mismatch", row)
			}
		}
		common := query.CTE("ranked_orders", q)
		a := query.As[rankedAlias](common, "ranked")
		fields := windowqueries.RankingRowFieldsAt(a.Scope())
		top := query.SelectRecord(a, a.Scope()).Where(fields.Number.Eq(1))
		if n, err := top.Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("outer filter did not consume typed window output", err)
		}
		if n, err := top.Union(top).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("window CTE/set composition failed", err)
		}
		if n, err := q.Limit(2).Offset(1).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("window result count lost limit/offset", err)
		}
		if row, err := q.Limit(0).First(t.Context(), tx); err != nil || row.IsSet() {
			t.Fatal("window first ignored zero limit", err)
		}
		if found, err := q.Limit(0).Exists(t.Context(), tx); err != nil || found {
			t.Fatal("window exists ignored zero limit", err)
		}
		stop := errors.New("stop window stream")
		if err := q.Each(t.Context(), tx, func(windowqueries.RankingRow) error { return stop }); !errors.Is(err, stop) {
			t.Fatal("window stream lost callback error", err)
		}
		if n, err := q.Count(t.Context(), tx); err != nil || n != 5 {
			t.Fatal("window stream did not release connection", err)
		}
		// DISTINCT ordering reuses the selected window, including NTILE parameters.
		base := models.QueryOrders()
		bucket := query.NTile(2, query.WindowFor(base).OrderBy(models.OrderFields().TotalCents.Asc()))
		if result, err := query.SelectValue(base, bucket).Distinct().OrderBy(bucket.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(result, []int32{1, 2}) {
			t.Fatal("distinct window value lost bindings", err)
		}
		// Window ordering may consume an ordinary aggregate over grouped rows.
		count := query.Count[models.Order]()
		rank := query.Rank(query.WindowFor(base).OrderBy(count.Desc()))
		if result, err := query.SelectValue(base, rank).GroupBy(models.OrderFields().BuyerID.Group()).OrderBy(rank.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(result, []int64{1, 2}) {
			t.Fatal("rank of grouped counts failed", err)
		}
		return nil
	})
}

func TestPostgresWindowFramesAndAggregates(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		o := models.OrderFields()
		base := models.QueryOrders().Where(o.BuyerID.Eq(users[0].ID))
		peers := query.WindowFor(base).OrderBy(o.TotalCents.Asc())
		ordered := peers.OrderBy(o.ID.Asc())
		for _, test := range []struct {
			name   string
			window query.Window[models.Order]
			sums   []string
			counts []int64
		}{
			{"default range", peers, []string{"1", "5", "5", "8"}, []int64{1, 3, 3, 4}},
			{"running rows", ordered.RowsBetween(query.UnboundedPreceding(), query.CurrentRow()), []string{"1", "3", "5", "8"}, []int64{1, 2, 3, 4}},
			{"previous row", ordered.RowsBetween(query.Preceding(1), query.CurrentRow()), []string{"1", "3", "4", "5"}, []int64{1, 2, 2, 2}},
			{"previous group", peers.GroupsBetween(query.Preceding(1), query.CurrentRow()), []string{"1", "5", "5", "7"}, []int64{1, 3, 3, 3}},
			{"following peers", peers.RangeBetween(query.CurrentRow(), query.UnboundedFollowing()), []string{"8", "7", "7", "3"}, []int64{4, 3, 3, 1}},
			{"exclude ties", peers.ExcludeTies(), []string{"1", "3", "3", "8"}, []int64{1, 2, 2, 4}},
			{"exclude group", peers.ExcludeGroup(), []string{"", "1", "1", "5"}, []int64{0, 1, 1, 3}},
			{"exclude current", ordered.RowsBetween(query.CurrentRow(), query.CurrentRow()).ExcludeCurrentRow(), []string{"", "", "", ""}, []int64{0, 0, 0, 0}},
			{"restore exclusion", peers.ExcludeTies().ExcludeNone(), []string{"1", "5", "5", "8"}, []int64{1, 3, 3, 4}},
			{"legal empty frame", ordered.RowsBetween(query.Preceding(7), query.Preceding(8)), []string{"", "", "", ""}, []int64{0, 0, 0, 0}},
		} {
			q := windowqueries.SelectWindowTotal(base, windowqueries.WindowTotalSelection[models.Order]{Amount: o.TotalCents.Value(), Running: o.TotalCents.Sum().Over(test.window), Count: query.Count[models.Order]().Over(test.window), Any: query.Exists[models.Order]().Over(test.window)}).OrderBy(o.TotalCents.Asc(), o.ID.Asc())
			rows, err := q.All(t.Context(), tx)
			if err != nil {
				return err
			}
			if len(rows) != 4 {
				t.Fatal(test.name, "lost frame rows")
			}
			for i, row := range rows {
				sum, valid := row.Running.Get()
				if valid != (test.sums[i] != "") || (valid && sum.String() != test.sums[i]) || row.Count != test.counts[i] || row.Any != (test.counts[i] > 0) {
					t.Fatal(test.name, "incorrect frame", i, row)
				}
			}
		}
		whole := ordered.RowsBetween(query.UnboundedPreceding(), query.UnboundedFollowing())
		average := o.TotalCents.Avg().Over(whole)
		if v, err := query.SelectValue(base, average).RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(v, value.Of(decimal.FromInt64(2))) {
			t.Fatal("exact window average lost decimal codec", err)
		}
		return nil
	})
}

func TestPostgresWindowNavigationAndNullability(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		o := models.OrderFields()
		base := models.QueryOrders().Where(o.BuyerID.Eq(users[0].ID))
		ordered := query.WindowFor(base).OrderBy(o.TotalCents.Asc(), o.ID.Asc())
		whole := ordered.RowsBetween(query.UnboundedPreceding(), query.UnboundedFollowing())
		single := ordered.RowsBetween(query.CurrentRow(), query.CurrentRow())
		q := windowqueries.SelectNavigationRow(base, windowqueries.NavigationRowSelection[models.Order]{Amount: o.TotalCents.Value(), Previous: query.Lag(o.TotalCents.Value(), 1, single), Next: query.Lead(o.TotalCents.Value(), 1, single), Fallback: query.LagOr(o.TotalCents.Value(), 1, int64(99), single), First: query.FirstValue(o.TotalCents.Value(), whole), Last: query.LastValue(o.TotalCents.Value(), whole), Nth: query.NthValue(o.TotalCents.Value(), 2, whole)}).OrderBy(o.TotalCents.Asc(), o.ID.Asc())
		rows, err := q.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(rows) != 4 {
			t.Fatal("navigation changed row count")
		}
		for i, row := range rows {
			previous := []value.Nullable[int64]{value.Null[int64](), value.Of[int64](1), value.Of[int64](2), value.Of[int64](2)}[i]
			next := []value.Nullable[int64]{value.Of[int64](2), value.Of[int64](2), value.Of[int64](3), value.Null[int64]()}[i]
			if row.Previous != previous || row.Next != next || row.Fallback != []int64{99, 1, 2, 2}[i] || row.First != value.Of[int64](1) || row.Last != value.Of[int64](3) || row.Nth != value.Of[int64](2) {
				t.Fatal("navigation ignored partition/frame/absence distinction", row)
			}
		}
		if values, err := query.SelectValue(base, query.Lag(o.TotalCents.Value(), -1, ordered)).OrderBy(o.TotalCents.Asc(), o.ID.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(values, []value.Nullable[int64]{value.Of[int64](2), value.Of[int64](2), value.Of[int64](3), value.Null[int64]()}) {
			t.Fatal("negative lag offset changed direction", err)
		}
		u := models.UserFields()
		people := models.QueryUsers().OrderBy(u.Email.Asc())
		w := query.WindowFor(people).OrderBy(u.Email.Asc())
		fallback := query.LagOr(u.Nickname.Value(), 1, value.Of("missing"), w)
		if values, err := query.SelectValue(people, fallback).All(t.Context(), tx); err != nil || !reflect.DeepEqual(values, []value.Nullable[string]{value.Of("missing"), value.Null[string](), value.Of("Bee")}) {
			t.Fatal("fallback replaced an existing NULL", err)
		}
		if values, err := query.SelectValue(people, query.LeadNullable(u.Nickname.Value(), 1, w)).All(t.Context(), tx); err != nil || !reflect.DeepEqual(values, []value.Nullable[string]{value.Of("Bee"), value.Null[string](), value.Null[string]()}) {
			t.Fatal("nullable lead lost single NULL wrapper", err)
		}
		return nil
	})
}

func TestPostgresCorrelatedAndRecursiveWindows(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		people := models.QueryUsers()
		orders := query.As[orderAlias](models.QueryOrders(), "purchase")
		link := query.Correlate(people, orders)
		p := models.UserFieldsAt(query.OuterScope(link, people.Scope()))
		o := models.OrderFieldsAt(query.InnerScope(link, orders.Scope()))
		counts := query.SelectCorrelatedValue(link, p.ID.Count().Over(query.WindowFor(link).PartitionBy(p.Status.Group()))).Where(o.BuyerID.EqColumn(p.ID)).Limit(1)
		if rows, err := query.SelectValue(people, query.CorrelatedScalarQuery(counts)).OrderBy(models.UserFields().Email.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []value.Nullable[int64]{value.Of[int64](4), value.Of[int64](1), value.Null[int64]()}) {
			t.Fatal("correlated window changed aggregate ownership", err)
		}
		tree := query.RecursiveCTE("window_tree", people.Where(models.UserFields().ID.Eq(users[0].ID)), func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			parent := query.As[parentAlias](self, "parent")
			child := query.As[childAlias](people, "child")
			pf, cf := models.UserFieldsAt(parent.Scope()), models.UserFieldsAt(child.Scope())
			joined := query.InnerJoin(child, parent, query.On(cf.IntroducerID, pf.ID))
			scope := query.LeftScope(joined, child.Scope())
			f := models.UserFieldsAt(scope)
			return query.SelectRecord(joined, scope).OrderBy(f.ID.Count().Over(query.WindowFor(joined)).Asc())
		})
		a := query.As[rankedAlias](tree, "tree_result")
		if n, err := query.SelectRecord(a, a.Scope()).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("recursive window was confused with forbidden ordinary aggregate", err)
		}
		return nil
	})
}

func TestInvalidWindowsNeverExecute(t *testing.T) {
	u := models.UserFields()
	base := models.QueryUsers()
	w := query.WindowFor(base).OrderBy(u.Email.Asc())
	for _, expression := range []query.Expression[models.User, int64]{query.RowNumber(w.OrderBy(query.RowNumber(w).Asc())), query.RowNumber(w.RowsBetween(query.Preceding(-1), query.CurrentRow())), u.ID.CountDistinct().Over(w)} {
		if rows, err := query.SelectValue(base, expression).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid window reached executor", err)
		}
	}
	bad := query.LagOr(u.Status.Value(), 1, models.Status("invalid"), w)
	if rows, err := query.SelectValue(base, bad).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid enum fallback reached executor", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := ranking().All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled window reached executor", err)
	}
}
