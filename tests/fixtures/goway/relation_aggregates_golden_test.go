package goway_test

import (
	"database/sql/driver"
	"fmt"
	"testing"

	j "foundry.test/goway/models/aggregates"
	old "foundry.test/goway/oldmodels/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func oldDirectTargets() query.ManyRelation[old.Parent, old.Target] {
	return query.HasMany(old.ParentFields().ID, old.TargetFields().ParentID).Bind("Targets", old.QueryJoinedParents().Query, old.QueryJoinedTargets().Query,
		func(m old.Parent) relation.Many[old.Target] { return m.Targets }, func(m old.Parent, v relation.Many[old.Target]) old.Parent { m.Targets = v; return m })
}

func relationAggregateGolden[V any](t *testing.T, before query.AggregateRelation[old.Parent, V], after orm.AggregateRelation[j.Parent, V], read func(j.Parent) relation.Value[V], raw driver.Value, extra int) {
	t.Helper()
	captures := []*goldenCapture{{}, {}}
	for side, g := range captures {
		g.reply = func(_ string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
			row := []driver.Value{int64(1), raw}
			for i := 0; i < extra; i++ {
				row = append(row, int64(1))
			}
			return make([]string, len(row)), [][]driver.Value{row}
		}
		db := g.open(t.Context())
		var result relation.Value[V]
		if side == 0 {
			r := before.Bind("Aggregate", old.QueryJoinedParents().Query, func(old.Parent) relation.Value[V] { return result }, func(m old.Parent, v relation.Value[V]) old.Parent { result = v; return m })
			_, err := old.QueryJoinedParents().With(r).Load(t.Context(), db, []old.Parent{{ID: 1}})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			parents, err := orm.Load(t.Context(), db, []j.Parent{{ID: 1}}, after)
			if err != nil {
				t.Fatal(err)
			}
			result = read(parents[0])
		}
		v, loaded := result.Get()
		fmt.Fprintf(&g.output, "value=%v loaded=%t\n", v, loaded)
		if !loaded || g.statements != 1 {
			t.Fatal("aggregate not loaded in one statement")
		}
		if err := db.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if !oracleEqual(t, "relation_aggregates_golden/1", captures[0].output.String(), captures[1].output.String()) {
		t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
	}
}

func TestRelationAggregateGoldens(t *testing.T) {
	counts := []struct {
		name    string
		old     query.AggregateSource[old.Parent, old.Target]
		current orm.AggregateSource[j.Parent, j.Target]
		extra   int
	}{
		{"direct", oldDirectTargets(), j.DirectTargets, 0},
		{"has_one", old.ParentRelations().Chosen, j.Chosen, 1},
		{"through", old.ParentRelations().Targets, j.TargetsThrough, 0},
		{"one_through", old.ParentRelations().ChosenThrough, j.ChosenThrough, 1},
		{"many_to_many", old.ParentRelations().Peers, j.Peers, 2},
		{"filtered_join", old.ParentRelations().Peers.Where(old.TargetFields().Rank.Gt(0)).WherePivot(old.PivotFields().Role.Eq("admin")).OnlyTrashedPivot(), j.Peers.Where(j.Targets.Rank.Gt(0)).WherePivot(j.Pivots.Role.Eq("admin")).OnlyTrashedPivot(), 2},
	}
	for _, tc := range counts {
		t.Run(tc.name, func(t *testing.T) {
			relationAggregateGolden(t, query.Related(tc.old, query.Count[old.Target]()), j.Parents.Count.Using(expr.Related(tc.current, expr.Count[j.Target]())), func(p j.Parent) relation.Value[int64] { return p.Count }, int64(1), tc.extra)
		})
	}
	t.Run("pivot", func(t *testing.T) {
		relationAggregateGolden(t, query.Related(old.ParentRelations().Peers.Pivot(), query.CountDistinctField(old.PivotFields().Role).Filter(old.PivotFields().Role.Eq("admin"))), j.Parents.Count.Using(expr.Related(orm.Pivot(j.Peers), expr.CountDistinctField(j.Pivots.Role).Filter(expr.Value(j.Pivots.Role).Eq("admin")))), func(p j.Parent) relation.Value[int64] { return p.Count }, int64(1), 2)
	})
	for _, singular := range []bool{false, true} {
		t.Run(fmt.Sprint("morph_", singular), func(t *testing.T) {
			var before query.AggregateSource[old.Parent, old.Pivot] = old.ParentRelations().MorphChildren
			var after orm.AggregateSource[j.Parent, j.Pivot] = j.MorphChildren
			extra := 0
			if singular {
				before = old.ParentRelations().MorphChild
				after = j.MorphChild
				extra = 1
			}
			relationAggregateGolden(t, query.Related(before, query.Count[old.Pivot]()), j.Parents.Count.Using(expr.Related(after, expr.Count[j.Pivot]())), func(p j.Parent) relation.Value[int64] { return p.Count }, int64(1), extra)
		})
	}
	for _, average := range []bool{false, true} {
		t.Run(fmt.Sprint("exact_", average), func(t *testing.T) {
			before := query.Sum(old.TargetFields().Rank)
			after := expr.Sum(j.Targets.Rank)
			if average {
				before = query.Avg(old.TargetFields().Rank)
				after = expr.Avg(j.Targets.Rank)
			}
			relationAggregateGolden(t, query.Related(oldDirectTargets(), before.Filter(old.TargetFields().Rank.Gt(0))), j.Parents.Total.Using(expr.Related(j.DirectTargets, after.Filter(expr.OrderRow(expr.Value(j.Targets.Rank)).Gt(0)))), func(p j.Parent) relation.Value[value.Nullable[decimal.Decimal]] { return p.Total }, "7", 0)
		})
	}
	t.Run("minimum", func(t *testing.T) {
		relationAggregateGolden(t, query.Related(oldDirectTargets(), query.Min(old.TargetFields().Name)), j.Parents.Minimum.Using(expr.Related(j.DirectTargets, expr.Min(j.Targets.Name))), func(p j.Parent) relation.Value[value.Nullable[string]] { return p.Minimum }, "seven", 0)
	})
	t.Run("maximum_null", func(t *testing.T) {
		relationAggregateGolden(t, query.Related(oldDirectTargets(), query.Max(old.TargetFields().Name)), j.Parents.Minimum.Using(expr.Related(j.DirectTargets, expr.Max(j.Targets.Name))), func(p j.Parent) relation.Value[value.Nullable[string]] { return p.Minimum }, nil, 0)
	})
	t.Run("exists", func(t *testing.T) {
		relationAggregateGolden(t, query.Related(oldDirectTargets(), query.Exists[old.Target]()), j.Parents.HasAny.Using(expr.Related(j.DirectTargets, expr.Exists[j.Target]())), func(p j.Parent) relation.Value[bool] { return p.HasAny }, true, 0)
	})
}
