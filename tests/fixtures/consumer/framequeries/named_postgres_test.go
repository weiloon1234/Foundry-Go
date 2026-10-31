package framequeries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
		base := expr.WindowFor(rowset.ModelScope(q)).PartitionBy(expr.Group(expr.Param(orm.NumberKeyExpr(f.ID), 1))).Named("all_samples")
		ordered := base.OrderBy(expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID)))).Named("ordered")
		framed := ordered.RowsBetween(expr.Preceding(1), expr.CurrentRow()).Named("framed")
		count := expr.Over(expr.Count[framequeries.Sample](), framed)
		result := rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), count), expr.AscValue(expr.RowNumber(framed)))
		if got, err := rowset.ReadScalar(result).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2, 2, 2}) {
			t.Fatal("named frame reuse", got, err)
		}
		statement, err := rowset.CompileRead(rowset.ReadScalar(result))
		if err != nil || strings.Count(statement.SQL(), `"framed" AS (`) != 1 || len(statement.Arguments()) != 2 {
			t.Fatal("named definition or parameter duplication", statement.SQL(), err)
		}
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.DistinctOnScalarValues(rowset.SelectScalar(rowset.From(q), count), expr.Key(count)), expr.AscValue(count), expr.AscValue(expr.RowNumber(framed)))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("named window distinct selection", got, err)
		}
		if total, err := rowset.ReadScalar(result).Count(t.Context(), tx); err != nil || total != 4 {
			t.Fatal("named result count", total, err)
		}
		r := expr.NumericRange(base, expr.AsSelected(expr.Row(expr.Value(orm.NumberKeyExpr(f.ID)))))
		frameCounts(t, tx, r.Between(r.Preceding(1), r.CurrentRow()).Named("ranged"), []int64{1, 2, 2, 2})
		bucket := expr.When(expr.ModelPredicate(orm.NumberKeyGt(f.ID, 2)), expr.Param(orm.NumberKeyExpr(f.ID), 1)).Else(expr.Param(orm.NumberKeyExpr(f.ID), 0))
		grouped := expr.WindowFor(rowset.ModelScope(q)).PartitionByValues(expr.Key(expr.AsSelected(expr.Count[framequeries.Sample]()))).Named("equal_counts")
		rank := expr.RowNumber(grouped)
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), rank), expr.Group(bucket)), expr.AscValue(rank))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
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
		eligible := rowset.AsRecord[eligibleSample](rowset.CTE("eligible_samples", rowset.ModelRecord(q.Where(orm.NumberKeyGt(f.ID, 2)))), "eligible")
		e := rowset.Scope(eligible)
		bucket := expr.When(rowset.InQuery(expr.Value(orm.NumberKeyExpr(f.ID)), rowset.SelectScalar(eligible, expr.AsSelected(expr.Row(expr.At(e, orm.NumberKeyExpr(framequeries.Samples.ID)))))), expr.Param(orm.NumberKeyExpr(f.ID), 1)).Else(expr.Param(orm.NumberKeyExpr(f.ID), 0))
		w := expr.WindowFor(rowset.ModelScope(q)).PartitionBy(expr.Group(bucket)).OrderBy(expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID)))).Named("buckets")
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), expr.RowNumber(w)), expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2, 1, 2}) {
			t.Fatal("CTE used only inside named definition", got, err)
		}
		inner := rowset.LimitScalar(rowset.SelectScalar(rowset.From(q), expr.RowNumber(expr.WindowFor(rowset.ModelScope(q)).OrderBy(expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID)))).Named("same"))), 1)
		outerWindow := expr.WindowFor(rowset.ModelScope(q)).OrderBy(expr.Desc(expr.Value(orm.NumberKeyExpr(f.ID)))).Named("same")
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), expr.AsSelected(rowset.ScalarRowQuery(rowset.From(q), inner))), expr.AscValue(expr.RowNumber(outerWindow)))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Of(int64(1)), value.Of(int64(1))}) {
			t.Fatal("named window SELECT namespace", got, err)
		}
		earlier := rowset.As[earlierSample](q, "earlier")
		link := rowset.Correlate(rowset.From(q), earlier)
		parent := rowset.OuterScope(link, rowset.ModelScope(q))
		child := rowset.InnerScope(link, rowset.Scope(earlier))
		perParent := expr.WindowFor(parent).PartitionBy(expr.Group(expr.At(parent, orm.NumberKeyExpr(framequeries.Samples.ID)))).Named("per_parent")
		counts := rowset.SelectCorrelatedScalar(link, expr.Over(expr.CountFieldAt(child, orm.NumberKeyExpr(framequeries.Samples.ID)), perParent)).Where(expr.Less(expr.At(child, orm.NumberKeyExpr(framequeries.Samples.ID)), expr.At(parent, orm.NumberKeyExpr(framequeries.Samples.ID)))).Limit(1)
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), expr.AsSelected(rowset.CorrelatedScalarRowQuery(counts))), expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Null[int64](), value.Of(int64(1)), value.Of(int64(2)), value.Of(int64(3))}) {
			t.Fatal("named window correlation", got, err)
		}
		return nil
	})
}
