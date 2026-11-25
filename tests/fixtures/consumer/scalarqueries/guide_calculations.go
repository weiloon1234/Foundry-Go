package scalarqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin scalar-calculations
func IncrementedAtLeast(minimum int16) orm.Query[Sample] {
	quantity := expr.Value(Samples.Quantity)
	incremented := expr.Add(quantity, expr.Param(Samples.Quantity, 1))
	return expr.Where(Samples.Query(), expr.Gte(incremented, minimum))
}

// doc-example:end scalar-calculations

// doc-example:begin collated-ordering
func SamplesByName() orm.Query[Sample] {
	name := expr.Collate(expr.Value(Samples.Name), expr.CollationUnicode)
	return expr.OrderBy(Samples.Query(), expr.Asc(name))
}

// doc-example:end collated-ordering

// doc-example:begin computed-text
type NoteSide struct{}

func NoteSearch(term Label) orm.Query[Sample] {
	source := rows.From(Samples.Query())
	lookup := rows.As[NoteSide](Samples.Query(), "note_lookup")
	correlation := rows.Correlate(source, lookup)
	outer := rows.OuterScope(correlation, rows.ModelScope(Samples.Query()))
	inner := rows.InnerScope(correlation, rows.Scope(lookup))
	scalar := rows.SelectCorrelatedScalar(correlation,
		expr.AsSelected(expr.Row(expr.At(inner, Samples.Note)))).
		Where(expr.Equal(expr.At(inner, Samples.ID), expr.At(outer, Samples.ID))).Limit(1)
	text := expr.TextNullableRowValue(rows.CorrelatedScalarNullableRowQuery(scalar))
	return expr.Where(Samples.Query(), expr.NullableTextIContains(text, term))
}

// doc-example:end computed-text
