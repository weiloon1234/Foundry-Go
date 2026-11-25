package temporalqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type CalendarSummaryColumns struct {
	rows.Row[CalendarSummary]
	Year   rows.Field[CalendarSummary, int64]
	Rows   rows.Field[CalendarSummary, int64]
	Latest rows.Field[CalendarSummary, value.Nullable[temporal.Date]]
}

var CalendarSummaries = rows.Define(func(b rows.Builder[CalendarSummary], s *CalendarSummary) CalendarSummaryColumns {
	return CalendarSummaryColumns{Row: b.Row(), Year: rows.NumberOf(b, &s.Year), Rows: rows.NumberOf(b, &s.Rows), Latest: rows.NullableTimeOf(b, &s.Latest)}
})

// doc-example:begin temporal-calculations
func YearlySummary() rows.Query[Sample, CalendarSummary] {
	year := expr.Year(expr.Value(Samples.Date))
	return rows.OrderBy(rows.GroupBy(rows.Select(rows.From(Samples.Query()), CalendarSummaries, rows.MapSelected(CalendarSummaries.Year, expr.AsSelected(year)), rows.MapSelected(CalendarSummaries.Rows, expr.AsSelected(expr.Count[Sample]())), rows.MapSelected(CalendarSummaries.Latest, expr.AsSelected(expr.Max(Samples.Date)))), expr.Group(year)), expr.Asc(year))
}

// doc-example:end temporal-calculations
