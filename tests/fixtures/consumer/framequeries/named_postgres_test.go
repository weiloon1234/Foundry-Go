package framequeries_test

import (
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/framequeries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresNamedWindowReuseAndFrames(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.Samples.Query()
		f := framequeries.Samples
		base := expr.WindowFor(rowset.ModelScope(q)).PartitionBy(expr.Group(expr.Param(f.ID, 1))).Named("all_samples")
		ordered := base.OrderBy(expr.Value(f.ID).Asc()).Named("ordered")
		framed := ordered.RowsBetween(expr.Preceding(1), expr.CurrentRow()).Named("framed")
		count := expr.Over(expr.Count[framequeries.Sample](), framed)
		result := rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), count), expr.RowNumber(framed).Asc())
		if got, err := rowset.ReadScalar(result).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2, 2, 2}) {
			t.Fatal("named frame reuse", got, err)
		}
		statement, err := rowset.CompileRead(rowset.ReadScalar(result))
		if err != nil || strings.Count(statement.SQL(), `"framed" AS (`) != 1 || len(statement.Arguments()) != 2 {
			t.Fatal("named definition or parameter duplication", statement.SQL(), err)
		}
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.DistinctOnScalarValues(rowset.SelectScalar(rowset.From(q), count), count.Key()), count.Asc(), expr.RowNumber(framed).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("named window distinct selection", got, err)
		}
		if total, err := rowset.ReadScalar(result).Count(t.Context(), tx); err != nil || total != 4 {
			t.Fatal("named result count", total, err)
		}
		r := expr.NumericRange(base, expr.Row(expr.Value(f.ID)).Value())
		frameCounts(t, tx, r.Between(r.Preceding(1), r.CurrentRow()).Named("ranged"), []int64{1, 2, 2, 2})
		bucket := expr.When(expr.ModelPredicate(f.ID.Gt(2)), expr.Param(f.ID, 1)).Else(expr.Param(f.ID, 0))
		grouped := expr.WindowFor(rowset.ModelScope(q)).PartitionByValues(expr.Count[framequeries.Sample]().Value().Key()).Named("equal_counts")
		rank := expr.RowNumber(grouped)
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), rank), expr.Group(bucket)), rank.Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("named aggregate partition grouping", got, err)
		}
		return nil
	})
}

type eligibleSample struct{}
type earlierSample struct{}

func TestPostgresNamedWindowScopesAndDependencies(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.Samples.Query()
		f := framequeries.Samples
		eligible := rowset.AsRecord[eligibleSample](rowset.CTE("eligible_samples", rowset.ModelRecord(q.Where(f.ID.Gt(2)))), "eligible")
		e := rowset.Scope(eligible)
		bucket := expr.When(rowset.InQuery(expr.Value(f.ID), rowset.SelectScalar(eligible, expr.Row(expr.At(e, framequeries.Samples.ID)).Value())), expr.Param(f.ID, 1)).Else(expr.Param(f.ID, 0))
		w := expr.WindowFor(rowset.ModelScope(q)).PartitionBy(expr.Group(bucket)).OrderBy(expr.Value(f.ID).Asc()).Named("buckets")
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), expr.RowNumber(w)), expr.Value(f.ID).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2, 1, 2}) {
			t.Fatal("CTE used only inside named definition", got, err)
		}
		inner := rowset.LimitScalar(rowset.SelectScalar(rowset.From(q), expr.RowNumber(expr.WindowFor(rowset.ModelScope(q)).OrderBy(expr.Value(f.ID).Asc()).Named("same"))), 1)
		outerWindow := expr.WindowFor(rowset.ModelScope(q)).OrderBy(expr.Value(f.ID).Desc()).Named("same")
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), rowset.ScalarQuery(rowset.From(q), inner)), expr.RowNumber(outerWindow).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Of(int64(1)), value.Of(int64(1))}) {
			t.Fatal("named window SELECT namespace", got, err)
		}
		earlier := rowset.As[earlierSample](q, "earlier")
		link := rowset.Correlate(rowset.From(q), earlier)
		parent := rowset.OuterScope(link, rowset.ModelScope(q))
		child := rowset.InnerScope(link, rowset.Scope(earlier))
		perParent := expr.WindowFor(parent).PartitionBy(expr.Group(expr.At(parent, framequeries.Samples.ID))).Named("per_parent")
		counts := rowset.SelectCorrelatedScalar(link, expr.Over(expr.CountFieldAt(child, framequeries.Samples.ID), perParent)).Where(expr.Less(expr.At(child, framequeries.Samples.ID), expr.At(parent, framequeries.Samples.ID))).Limit(1)
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), rowset.CorrelatedScalarQuery(counts)), expr.Value(f.ID).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Null[int64](), value.Of(int64(1)), value.Of(int64(2)), value.Of(int64(3))}) {
			t.Fatal("named window correlation", got, err)
		}
		return nil
	})
}
