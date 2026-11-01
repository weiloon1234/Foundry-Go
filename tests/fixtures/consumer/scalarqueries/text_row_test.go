package scalarqueries_test

import (
	"testing"

	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// guide:computed-text-row
type noteSide struct{}

func noteSearch(term scalarqueries.Label) orm.Query[scalarqueries.Sample] {
	source := rows.From(scalarqueries.Samples.Query())
	lookup := rows.As[noteSide](scalarqueries.Samples.Query(), "note_lookup")
	correlation := rows.Correlate(source, lookup)
	outer := rows.OuterScope(correlation, rows.ModelScope(scalarqueries.Samples.Query()))
	inner := rows.InnerScope(correlation, rows.Scope(lookup))
	scalar := rows.SelectCorrelatedScalar(correlation,
		expr.AsSelected(expr.Row(expr.At(inner, orm.NullableTextExpr(scalarqueries.Samples.Note))))).
		Where(expr.Equal(expr.At(inner, orm.NumberKeyExpr(scalarqueries.Samples.ID)), expr.At(outer, orm.NumberKeyExpr(scalarqueries.Samples.ID)))).Limit(1)
	text := expr.TextNullableRowValue(rows.CorrelatedScalarNullableRowQuery(scalar))
	return expr.Where(scalarqueries.Samples.Query(), expr.NullableTextIContains(text, term))
}

// guide:end

func TestComputedTextRowGuide(t *testing.T) {
	if _, err := rows.CompileRead(rows.Read(rows.FromModel(noteSearch("AL")))); err != nil {
		t.Fatal(err)
	}
}
