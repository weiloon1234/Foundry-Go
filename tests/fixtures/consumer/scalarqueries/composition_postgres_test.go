package scalarqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"

	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type computedLines struct{}
type matchingSample struct{}
type previousSample struct{}

func TestPostgresScalarCTEDiscoveryAndCorrelations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.Samples.Query()
		f := scalarqueries.Samples
		matching := rowset.AsRecord[matchingSample](rowset.CTE("positive_quantity", rowset.ModelRecord(expr.Where(q, expr.Abs(expr.Value(orm.NumberExpr(f.Quantity))).Gt(2)))), "matching")
		m := rowset.Scope(matching)
		matched := expr.When(rowset.InQuery(expr.Value(orm.NumberKeyExpr(f.ID)), rowset.SelectScalar(matching, expr.Row(expr.At(m, orm.NumberKeyExpr(scalarqueries.Samples.ID))).Value())), expr.Param(orm.NumberKeyExpr(f.ID), 10)).Else(expr.Param(orm.NumberKeyExpr(f.ID), 0))
		scalarValues(t, tx, expr.Add(matched, expr.Value(orm.NumberKeyExpr(f.ID))).Value(), []int{1, 12, 13})
		previous := rowset.As[previousSample](q, "previous")
		link := rowset.Correlate(rowset.From(q), previous)
		parent := rowset.OuterScope(link, rowset.ModelScope(q))
		child := rowset.InnerScope(link, rowset.Scope(previous))
		caption := expr.Concat(expr.Lower(expr.At(parent, orm.TextExpr(scalarqueries.Samples.Name))), expr.Lower(expr.At(child, orm.TextExpr(scalarqueries.Samples.Name))))
		correlated := rowset.SelectCorrelatedScalar(link, caption.Value()).Where(expr.Less(expr.At(child, orm.NumberKeyExpr(scalarqueries.Samples.ID)), expr.At(parent, orm.NumberKeyExpr(scalarqueries.Samples.ID)))).OrderBy(expr.At(child, orm.NumberKeyExpr(scalarqueries.Samples.ID)).Desc()).Limit(1)
		scalarValues(t, tx, rowset.CorrelatedScalarRowQuery(correlated).Value(), []value.Nullable[string]{value.Null[string](), value.Of("beta élan_20% "), value.Of("beta")})
		return nil
	})
}
