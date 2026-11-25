package framequeries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// doc-example:begin window-ranges-and-names
func IDRange() (expr.RangeWindow[Sample, int], expr.Window[Sample]) {
	r := expr.NumericRange(expr.WindowFor(rows.ModelScope(Samples.Query())), expr.AsSelected(expr.Row(expr.Value(Samples.ID))))
	return r, r.Between(r.Preceding(1), r.CurrentRow())
}

// doc-example:end window-ranges-and-names

// doc-example:begin window-ranges-and-names-2
func DayWindows() (expr.Window[Sample], expr.Window[Sample], error) {
	r := expr.TemporalRange(expr.WindowFor(rows.ModelScope(Samples.Query())), expr.AsSelected(expr.Row(expr.Value(Samples.At))))
	day, err := temporal.Elapsed(24 * time.Hour)
	if err != nil {
		return expr.Window[Sample]{}, expr.Window[Sample]{}, err
	}
	return r.Between(r.Preceding(temporal.Days(1)), r.CurrentRow()), r.Between(r.Preceding(day), r.CurrentRow()), nil
}

// doc-example:end window-ranges-and-names-2

// NamedWindows builds the guide's chained named windows and the count over the last.
func NamedWindows() (expr.Window[Sample], expr.Window[Sample], expr.Selected[Sample, int64], rows.Scalar[Sample, int64]) {
	// doc-example:begin window-ranges-and-names-3
	q := Samples.Query()
	base := expr.WindowFor(rows.ModelScope(q)).PartitionBy(expr.Group(expr.Param(Samples.ID, 1))).Named("all_samples")
	ordered := base.OrderBy(expr.Asc(expr.Value(Samples.ID))).Named("ordered")
	framed := ordered.RowsBetween(expr.Preceding(1), expr.CurrentRow()).Named("framed")
	count := expr.Over(expr.Count[Sample](), framed)
	result := rows.OrderScalar(rows.SelectScalar(rows.From(q), count), expr.AscValue(expr.RowNumber(framed)))
	// doc-example:end window-ranges-and-names-3
	return base, framed, count, result
}
