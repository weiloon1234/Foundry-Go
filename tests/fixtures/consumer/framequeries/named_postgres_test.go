package framequeries_test

import (
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/framequeries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresNamedWindowReuseAndFrames(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.QueryFrameSamples()
		f := framequeries.SampleFields()
		base := query.WindowFor(q).PartitionBy(f.ID.Param(1).Group()).Named("all_samples")
		ordered := base.OrderBy(f.ID.Asc()).Named("ordered")
		framed := ordered.RowsBetween(query.Preceding(1), query.CurrentRow()).Named("framed")
		count := query.Count[framequeries.Sample]().Over(framed)
		result := query.SelectValue(q, count).OrderBy(query.RowNumber(framed).Asc())
		if got, err := result.All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2, 2, 2}) {
			t.Fatal("named frame reuse", got, err)
		}
		statement, err := result.Compile()
		if err != nil || strings.Count(statement.SQL(), `"framed" AS (`) != 1 || len(statement.Arguments()) != 2 {
			t.Fatal("named definition or parameter duplication", statement.SQL(), err)
		}
		if got, err := query.SelectValue(q, count).DistinctOnValues(count.Key()).OrderBy(count.Asc(), query.RowNumber(framed).Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("named window distinct selection", got, err)
		}
		if total, err := result.Count(t.Context(), tx); err != nil || total != 4 {
			t.Fatal("named result count", total, err)
		}
		r := query.NumericRange(base, f.ID.Value())
		frameCounts(t, tx, r.Between(r.Preceding(1), r.CurrentRow()).Named("ranged"), []int64{1, 2, 2, 2})
		bucket := query.When(f.ID.Gt(2), f.ID.Param(1)).Else(f.ID.Param(0))
		grouped := query.WindowFor(q).PartitionByValues(query.Count[framequeries.Sample]().Value().Key()).Named("equal_counts")
		rank := query.RowNumber(grouped)
		if got, err := query.SelectValue(q, rank).GroupBy(bucket.Group()).OrderBy(rank.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("named aggregate partition grouping", got, err)
		}
		return nil
	})
}

type eligibleSample struct{}
type earlierSample struct{}

func TestPostgresNamedWindowScopesAndDependencies(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.QueryFrameSamples()
		f := framequeries.SampleFields()
		eligible := query.As[eligibleSample](query.CTE("eligible_samples", q.Where(f.ID.Gt(2))), "eligible")
		e := framequeries.SampleFieldsAt(eligible.Scope())
		bucket := query.When(f.ID.InQuery(query.SelectValue(eligible, e.ID.Value())), f.ID.Param(1)).Else(f.ID.Param(0))
		w := query.WindowFor(q).PartitionBy(bucket.Group()).OrderBy(f.ID.Asc()).Named("buckets")
		if got, err := query.SelectValue(q, query.RowNumber(w)).OrderBy(f.ID.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2, 1, 2}) {
			t.Fatal("CTE used only inside named definition", got, err)
		}
		inner := query.SelectValue(q, query.RowNumber(query.WindowFor(q).OrderBy(f.ID.Asc()).Named("same"))).Limit(1)
		outerWindow := query.WindowFor(q).OrderBy(f.ID.Desc()).Named("same")
		if got, err := query.SelectValue(q, query.ScalarQuery(q, inner)).OrderBy(query.RowNumber(outerWindow).Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Of(int64(1)), value.Of(int64(1))}) {
			t.Fatal("named window SELECT namespace", got, err)
		}
		earlier := query.As[earlierSample](q, "earlier")
		link := query.Correlate(q, earlier)
		parent := framequeries.SampleFieldsAt(query.OuterScope(link, q.Scope()))
		child := framequeries.SampleFieldsAt(query.InnerScope(link, earlier.Scope()))
		perParent := query.WindowFor(link).PartitionBy(parent.ID.Group()).Named("per_parent")
		counts := query.SelectCorrelatedValue(link, child.ID.Count().Over(perParent)).Where(child.ID.LtColumn(parent.ID)).Limit(1)
		if got, err := query.SelectValue(q, query.CorrelatedScalarQuery(counts)).OrderBy(f.ID.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Null[int64](), value.Of(int64(1)), value.Of(int64(2)), value.Of(int64(3))}) {
			t.Fatal("named window correlation", got, err)
		}
		return nil
	})
}
