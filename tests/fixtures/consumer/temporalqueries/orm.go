package temporalqueries

import (
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
