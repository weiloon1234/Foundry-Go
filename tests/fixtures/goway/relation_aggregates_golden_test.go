package goway_test

import (
	"database/sql/driver"
	"fmt"
	"testing"

	j "foundry.test/goway/models/aggregates"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func relationAggregateGolden[V any](t *testing.T, after orm.AggregateRelation[j.Parent, V], read func(j.Parent) relation.Value[V], raw driver.Value, extra int) {
	t.Helper()
	captures := []*goldenCapture{{}}
	for _, g := range captures {
		g.reply = func(_ string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
			row := []driver.Value{int64(1), raw}
			for i := 0; i < extra; i++ {
				row = append(row, int64(1))
			}
			return make([]string, len(row)), [][]driver.Value{row}
		}
		db := g.open(t.Context())
		var result relation.Value[V]

		parents, err := orm.Load(t.Context(), db, []j.Parent{{ID: 1}}, after)
		if err != nil {
			t.Fatal(err)
		}
		result = read(parents[0])

		v, loaded := result.Get()
		fmt.Fprintf(&g.output, "value=%v loaded=%t\n", v, loaded)
		if !loaded || g.statements != 1 {
			t.Fatal("aggregate not loaded in one statement")
		}
		if err := db.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if !oracleEqual(t, "relation_aggregates_golden/1", captures[0].output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func TestRelationAggregateGoldens(t *testing.T) {
	counts := []struct {
		name string

		current orm.AggregateSource[j.Parent, j.Target]
		extra   int
	}{
		{"direct", j.DirectTargets, 0},
		{"has_one", j.Chosen, 1},
		{"through", j.TargetsThrough, 0},
		{"one_through", j.ChosenThrough, 1},
		{"many_to_many", j.Peers, 2},
		{"filtered_join", j.Peers.Where(j.Targets.Rank.Gt(0)).WherePivot(j.Pivots.Role.Eq("admin")).OnlyTrashedPivot(), 2},
	}
	for _, tc := range counts {
		t.Run(tc.name, func(t *testing.T) {
			relationAggregateGolden(t, j.Parents.Count.Using(expr.Related(tc.current, expr.Count[j.Target]())), func(p j.Parent) relation.Value[int64] { return p.Count }, int64(1), tc.extra)
		})
	}
	t.Run("pivot", func(t *testing.T) {
		relationAggregateGolden(t, j.Parents.Count.Using(expr.Related(orm.Pivot(j.Peers), expr.CountDistinctField(j.Pivots.Role).Filter(expr.Value(j.Pivots.Role).Eq("admin")))), func(p j.Parent) relation.Value[int64] { return p.Count }, int64(1), 2)
	})
	for _, singular := range []bool{false, true} {
		t.Run(fmt.Sprint("morph_", singular), func(t *testing.T) {
			var after orm.AggregateSource[j.Parent, j.Pivot] = j.MorphChildren
			extra := 0
			if singular {
				after = j.MorphChild
				extra = 1
			}
			relationAggregateGolden(t, j.Parents.Count.Using(expr.Related(after, expr.Count[j.Pivot]())), func(p j.Parent) relation.Value[int64] { return p.Count }, int64(1), extra)
		})
	}
	for _, average := range []bool{false, true} {
		t.Run(fmt.Sprint("exact_", average), func(t *testing.T) {
			after := expr.Sum(j.Targets.Rank)
			if average {
				after = expr.Avg(j.Targets.Rank)
			}
			relationAggregateGolden(t, j.Parents.Total.Using(expr.Related(j.DirectTargets, after.Filter(expr.OrderRow(expr.Value(j.Targets.Rank)).Gt(0)))), func(p j.Parent) relation.Value[value.Nullable[decimal.Decimal]] { return p.Total }, "7", 0)
		})
	}
	t.Run("minimum", func(t *testing.T) {
		relationAggregateGolden(t, j.Parents.Minimum.Using(expr.Related(j.DirectTargets, expr.Min(j.Targets.Name))), func(p j.Parent) relation.Value[value.Nullable[string]] { return p.Minimum }, "seven", 0)
	})
	t.Run("maximum_null", func(t *testing.T) {
		relationAggregateGolden(t, j.Parents.Minimum.Using(expr.Related(j.DirectTargets, expr.Max(j.Targets.Name))), func(p j.Parent) relation.Value[value.Nullable[string]] { return p.Minimum }, nil, 0)
	})
	t.Run("exists", func(t *testing.T) {
		relationAggregateGolden(t, j.Parents.HasAny.Using(expr.Related(j.DirectTargets, expr.Exists[j.Target]())), func(p j.Parent) relation.Value[bool] { return p.HasAny }, true, 0)
	})
}
