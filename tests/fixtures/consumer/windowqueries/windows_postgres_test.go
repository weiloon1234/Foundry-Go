package windowqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
			if _, err := orm.Insert(t.Context(), tx, models.Orders.Table, orm.ColumnSet(models.Orders.BuyerID, users[0].ID), orm.NumberSet(models.Orders.TotalCents, amount)); err != nil {
				return err
			}
		}
		return check(tx, users)
	})
}

func ranking() rows.Query[models.Order, windowqueries.RankingRow] {
	base := models.Orders.Query()
	o := models.Orders
	peers := expr.WindowFor(rows.ModelScope(base)).PartitionBy(expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))))
	ordered := peers.OrderBy(expr.Asc(expr.Value(orm.KeyExpr(o.ID))))
	return rows.OrderBy(rows.Select(rows.From(base), windowqueries.Rankings, rows.MapSelected(windowqueries.Rankings.ID, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(o.ID))))), rows.MapSelected(windowqueries.Rankings.BuyerID, expr.AsSelected(expr.Row(expr.Value(orm.ColumnExpr(o.BuyerID))))), rows.MapSelected(windowqueries.Rankings.Number, expr.RowNumber(ordered)), rows.MapSelected(windowqueries.Rankings.Rank, expr.Rank(peers)), rows.MapSelected(windowqueries.Rankings.Dense, expr.DenseRank(peers)), rows.MapSelected(windowqueries.Rankings.Percent, expr.PercentRank(peers)), rows.MapSelected(windowqueries.Rankings.Cumulative, expr.CumeDist(peers)), rows.MapSelected(windowqueries.Rankings.Bucket, expr.NTile(2, ordered))), expr.Asc(expr.Value(orm.ColumnExpr(o.BuyerID))), expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))), expr.Asc(expr.Value(orm.KeyExpr(o.ID))))
}

func TestPostgresWindowRankingAndComposition(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		q := ranking()
		items, err := rows.Read(q).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(items) != 5 {
			t.Fatal("ranking collapsed items into groups")
		}
		var first []windowqueries.RankingRow
		for _, row := range items {
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
		common := rows.CTE("ranked_orders", rows.RecordOf(q))
		a := rows.AsRecord[rankedAlias](common, "ranked")
		fields := rows.Scope(a)
		top := rows.SelectRecord(a, rows.Scope(a)).Where(expr.Eq(rows.At(fields, windowqueries.Rankings.Number), 1))
		if n, err := rows.ReadRecord(top).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("outer filter did not consume typed window output", err)
		}
		if n, err := rows.ReadSet(rows.Union(top, top)).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("window CTE/set composition failed", err)
		}
		if n, err := rows.Read(rows.Offset(rows.Limit(q, 2), 1)).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("window result count lost limit/offset", err)
		}
		if row, err := rows.Read(rows.Limit(q, 0)).First(t.Context(), tx); err != nil || row.IsSet() {
			t.Fatal("window first ignored zero limit", err)
		}
		if found, err := rows.Read(rows.Limit(q, 0)).Exists(t.Context(), tx); err != nil || found {
			t.Fatal("window exists ignored zero limit", err)
		}
		stop := errors.New("stop window stream")
		if err := rows.EachRead(t.Context(), tx, rows.Read(q), func(windowqueries.RankingRow) error { return stop }); !errors.Is(err, stop) {
			t.Fatal("window stream lost callback error", err)
		}
		if n, err := rows.Read(q).Count(t.Context(), tx); err != nil || n != 5 {
			t.Fatal("window stream did not release connection", err)
		}
		// DISTINCT ordering reuses the selected window, including NTILE parameters.
		base := models.Orders.Query()
		bucket := expr.NTile(2, expr.WindowFor(rows.ModelScope(base)).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(models.Orders.TotalCents)))))
		if result, err := rows.ReadScalar(rows.SelectScalar(rows.OrderBy(rows.Distinct(rows.FromModel(base)), expr.AscValue(bucket)), bucket)).All(t.Context(), tx); err != nil || !reflect.DeepEqual(result, []int32{1, 2}) {
			t.Fatal("distinct window value lost bindings", err)
		}
		// Window ordering may consume an ordinary aggregate over grouped rows.
		count := expr.Count[models.Order]()
		rank := expr.Rank(expr.WindowFor(rows.ModelScope(base)).OrderBy(expr.DescValue(count)))
		if result, err := rows.ReadScalar(rows.SelectScalar(rows.OrderBy(rows.GroupBy(rows.FromModel(base), expr.Group(expr.Value(orm.ColumnExpr(models.Orders.BuyerID)))), expr.AscValue(rank)), rank)).All(t.Context(), tx); err != nil || !reflect.DeepEqual(result, []int64{1, 2}) {
			t.Fatal("rank of grouped counts failed", err)
		}
		return nil
	})
}

func TestPostgresWindowFramesAndAggregates(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		o := models.Orders
		base := models.Orders.Query().Where(orm.ColumnEq(o.BuyerID, users[0].ID))
		peers := expr.WindowFor(rows.ModelScope(base)).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))))
		ordered := peers.OrderBy(expr.Asc(expr.Value(orm.KeyExpr(o.ID))))
		for _, test := range []struct {
			name   string
			window expr.Window[models.Order]
			sums   []string
			counts []int64
		}{
			{"default range", peers, []string{"1", "5", "5", "8"}, []int64{1, 3, 3, 4}},
			{"running items", ordered.RowsBetween(expr.UnboundedPreceding(), expr.CurrentRow()), []string{"1", "3", "5", "8"}, []int64{1, 2, 3, 4}},
			{"previous row", ordered.RowsBetween(expr.Preceding(1), expr.CurrentRow()), []string{"1", "3", "4", "5"}, []int64{1, 2, 2, 2}},
			{"previous group", peers.GroupsBetween(expr.Preceding(1), expr.CurrentRow()), []string{"1", "5", "5", "7"}, []int64{1, 3, 3, 3}},
			{"following peers", peers.RangeBetween(expr.CurrentRow(), expr.UnboundedFollowing()), []string{"8", "7", "7", "3"}, []int64{4, 3, 3, 1}},
			{"exclude ties", peers.ExcludeTies(), []string{"1", "3", "3", "8"}, []int64{1, 2, 2, 4}},
			{"exclude group", peers.ExcludeGroup(), []string{"", "1", "1", "5"}, []int64{0, 1, 1, 3}},
			{"exclude current", ordered.RowsBetween(expr.CurrentRow(), expr.CurrentRow()).ExcludeCurrentRow(), []string{"", "", "", ""}, []int64{0, 0, 0, 0}},
			{"restore exclusion", peers.ExcludeTies().ExcludeNone(), []string{"1", "5", "5", "8"}, []int64{1, 3, 3, 4}},
			{"legal empty frame", ordered.RowsBetween(expr.Preceding(7), expr.Preceding(8)), []string{"", "", "", ""}, []int64{0, 0, 0, 0}},
		} {
			q := rows.OrderBy(rows.Select(rows.From(base), windowqueries.WindowTotals, rows.MapSelected(windowqueries.WindowTotals.Amount, expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents))))), rows.MapSelected(windowqueries.WindowTotals.Running, expr.Over(expr.Sum(orm.NumberExpr(o.TotalCents)), test.window)), rows.MapSelected(windowqueries.WindowTotals.Count, expr.Over(expr.Count[models.Order](), test.window)), rows.MapSelected(windowqueries.WindowTotals.Any, expr.Over(expr.Exists[models.Order](), test.window))), expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))), expr.Asc(expr.Value(orm.KeyExpr(o.ID))))
			items, err := rows.Read(q).All(t.Context(), tx)
			if err != nil {
				return err
			}
			if len(items) != 4 {
				t.Fatal(test.name, "lost frame items")
			}
			for i, row := range items {
				sum, valid := row.Running.Get()
				if valid != (test.sums[i] != "") || (valid && sum.String() != test.sums[i]) || row.Count != test.counts[i] || row.Any != (test.counts[i] > 0) {
					t.Fatal(test.name, "incorrect frame", i, row)
				}
			}
		}
		whole := ordered.RowsBetween(expr.UnboundedPreceding(), expr.UnboundedFollowing())
		average := expr.Over(expr.Avg(orm.NumberExpr(o.TotalCents)), whole)
		if v, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), average)).RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(v, value.Of(decimal.FromInt64(2))) {
			t.Fatal("exact window average lost decimal codec", err)
		}
		return nil
	})
}

func TestPostgresWindowNavigationAndNullability(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		o := models.Orders
		base := models.Orders.Query().Where(orm.ColumnEq(o.BuyerID, users[0].ID))
		ordered := expr.WindowFor(rows.ModelScope(base)).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))), expr.Asc(expr.Value(orm.KeyExpr(o.ID))))
		whole := ordered.RowsBetween(expr.UnboundedPreceding(), expr.UnboundedFollowing())
		single := ordered.RowsBetween(expr.CurrentRow(), expr.CurrentRow())
		q := rows.OrderBy(rows.Select(rows.From(base), windowqueries.Navigation, rows.MapSelected(windowqueries.Navigation.Amount, expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents))))), rows.MapSelected(windowqueries.Navigation.Previous, expr.Lag(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents)))), 1, single)), rows.MapSelected(windowqueries.Navigation.Next, expr.Lead(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents)))), 1, single)), rows.MapSelected(windowqueries.Navigation.Fallback, expr.LagOr(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents)))), 1, int64(99), single)), rows.MapSelected(windowqueries.Navigation.First, expr.FirstValue(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents)))), whole)), rows.MapSelected(windowqueries.Navigation.Last, expr.LastValue(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents)))), whole)), rows.MapSelected(windowqueries.Navigation.Nth, expr.NthValue(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents)))), 2, whole))), expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))), expr.Asc(expr.Value(orm.KeyExpr(o.ID))))
		items, err := rows.Read(q).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(items) != 4 {
			t.Fatal("navigation changed row count")
		}
		for i, row := range items {
			previous := []value.Nullable[int64]{value.Null[int64](), value.Of[int64](1), value.Of[int64](2), value.Of[int64](2)}[i]
			next := []value.Nullable[int64]{value.Of[int64](2), value.Of[int64](2), value.Of[int64](3), value.Null[int64]()}[i]
			if row.Previous != previous || row.Next != next || row.Fallback != []int64{99, 1, 2, 2}[i] || row.First != value.Of[int64](1) || row.Last != value.Of[int64](3) || row.Nth != value.Of[int64](2) {
				t.Fatal("navigation ignored partition/frame/absence distinction", row)
			}
		}
		if values, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expr.Lag(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents)))), -1, ordered))).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))), expr.Asc(expr.Value(orm.KeyExpr(o.ID)))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(values, []value.Nullable[int64]{value.Of[int64](2), value.Of[int64](2), value.Of[int64](3), value.Null[int64]()}) {
			t.Fatal("negative lag offset changed direction", err)
		}
		u := models.Users
		people := models.Users.Query().OrderBy(orm.TextAsc(u.Email))
		w := expr.WindowFor(rows.ModelScope(people)).OrderBy(expr.Asc(expr.Value(orm.TextExpr(u.Email))))
		fallback := expr.LagOr(expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(u.Nickname)))), 1, value.Of("missing"), w)
		if values, err := rows.ReadScalar(rows.SelectScalar(rows.From(people), fallback)).All(t.Context(), tx); err != nil || !reflect.DeepEqual(values, []value.Nullable[string]{value.Of("missing"), value.Null[string](), value.Of("Bee")}) {
			t.Fatal("fallback replaced an existing NULL", err)
		}
		if values, err := rows.ReadScalar(rows.SelectScalar(rows.From(people), expr.LeadNullable(expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(u.Nickname)))), 1, w))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(values, []value.Nullable[string]{value.Of("Bee"), value.Null[string](), value.Null[string]()}) {
			t.Fatal("nullable lead lost single NULL wrapper", err)
		}
		return nil
	})
}

func TestPostgresCorrelatedAndRecursiveWindows(t *testing.T) {
	runWindows(t, func(tx *database.Tx, users []models.User) error {
		people := models.Users.Query()
		orders := rows.As[orderAlias](models.Orders.Query(), "purchase")
		link := rows.Correlate(rows.From(people), orders)
		p := rows.OuterScope(link, rows.ModelScope(people))
		o := rows.InnerScope(link, rows.Scope(orders))
		counts := rows.SelectCorrelatedScalar(link, expr.Over(expr.CountFieldAt(p, orm.KeyExpr(models.Users.ID)), expr.WindowFor(p).PartitionBy(expr.Group(expr.At(p, orm.EnumExpr(models.Users.Status)))))).Where(expr.Equal(expr.At(o, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(p, orm.KeyExpr(models.Users.ID)))).Limit(1)
		if items, err := rows.ReadScalar(rows.SelectScalar(rows.From(people), expr.AsSelected(rows.CorrelatedScalarRowQuery(counts)))).OrderBy(expr.Asc(expr.Value(orm.TextExpr(models.Users.Email)))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(items, []value.Nullable[int64]{value.Of[int64](4), value.Of[int64](1), value.Null[int64]()}) {
			t.Fatal("correlated window changed aggregate ownership", err)
		}
		tree := rows.RecursiveCTE("window_tree", rows.ModelRecord(people.Where(orm.KeyEq(models.Users.ID, users[0].ID))), func(self rows.RecursiveSelf[models.User]) rows.RecordSource[models.User] {
			parent := rows.AsRecord[parentAlias](self, "parent")
			child := rows.As[childAlias](people, "child")
			pf, cf := rows.Scope(parent), rows.Scope(child)
			joined := rows.InnerJoin(child, parent, rows.On(expr.At(cf, orm.NullableColumnExpr(models.Users.IntroducerID)), expr.At(pf, orm.KeyExpr(models.Users.ID))))
			scope := rows.LeftScope(joined, rows.Scope(child))
			return rows.SelectRecord(joined, scope).OrderBy(expr.AscValue(expr.Over(expr.CountFieldAt(scope, orm.KeyExpr(models.Users.ID)), expr.WindowFor(scope))))
		})
		a := rows.AsRecord[rankedAlias](tree, "tree_result")
		if n, err := rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a))).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("recursive window was confused with forbidden ordinary aggregate", err)
		}
		return nil
	})
}

func TestInvalidWindowsNeverExecute(t *testing.T) {
	u := models.Users
	base := models.Users.Query()
	w := expr.WindowFor(rows.ModelScope(base)).OrderBy(expr.Asc(expr.Value(orm.TextExpr(u.Email))))
	for _, expression := range []expr.Selected[models.User, int64]{expr.RowNumber(w.OrderBy(expr.AscValue(expr.RowNumber(w)))), expr.RowNumber(w.RowsBetween(expr.Preceding(-1), expr.CurrentRow())), expr.Over(expr.CountDistinctField(orm.KeyExpr(u.ID)), w)} {
		if items, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expression)).All(t.Context(), queryfixture.NoQueries(t)); items != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid window reached executor", err)
		}
	}
	bad := expr.LagOr(expr.AsSelected(expr.Row(expr.Value(orm.EnumExpr(u.Status)))), 1, models.Status("invalid"), w)
	if items, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), bad)).All(t.Context(), queryfixture.NoQueries(t)); items != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid enum fallback reached executor", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if items, err := rows.Read(ranking()).All(ctx, queryfixture.NoQueries(t)); items != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled window reached executor", err)
	}
}
