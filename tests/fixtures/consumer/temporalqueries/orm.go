package temporalqueries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type SampleColumns struct {
	orm.Table[Sample, int]
	ID         orm.NumberPrimary[Sample, int]
	Millis     orm.Number[Sample, int64]
	At         orm.Time[Sample, time.Time]
	Instant    orm.Time[Sample, temporal.DateTime]
	Date       orm.Time[Sample, temporal.Date]
	Local      orm.Time[Sample, temporal.LocalDateTime]
	Clock      orm.Time[Sample, temporal.Time]
	MaybeAt    orm.NullableTime[Sample, time.Time]
	MaybeDate  orm.NullableTime[Sample, temporal.Date]
	MaybeLocal orm.NullableTime[Sample, temporal.LocalDateTime]
	MaybeClock orm.NullableTime[Sample, temporal.Time]
}

var Samples = orm.Define("temporal_samples", func(b orm.Builder[Sample], s *Sample) SampleColumns {
	table := orm.TableOf(b, &s.ID)
	return SampleColumns{Table: table, ID: orm.NumberKey(orm.PrimaryOf(b, table.Key())), Millis: orm.NumberOf(b, &s.Millis),
		At:         orm.TimeOf(b, &s.At),
		Instant:    orm.TimeOf(b, &s.Instant),
		Date:       orm.TimeOf(b, &s.Date),
		Local:      orm.TimeOf(b, &s.Local),
		Clock:      orm.TimeOf(b, &s.Clock),
		MaybeAt:    orm.NullableTimeOf(b, &s.MaybeAt),
		MaybeDate:  orm.NullableTimeOf(b, &s.MaybeDate),
		MaybeLocal: orm.NullableTimeOf(b, &s.MaybeLocal),
		MaybeClock: orm.NullableTimeOf(b, &s.MaybeClock),
	}
})

type CalendarSummaryColumns struct {
	rows.Row[CalendarSummary]
	Year   rows.Field[CalendarSummary, int64]
	Rows   rows.Field[CalendarSummary, int64]
	Latest rows.Field[CalendarSummary, value.Nullable[temporal.Date]]
}

var CalendarSummaries = rows.Define(func(b rows.Builder[CalendarSummary], s *CalendarSummary) CalendarSummaryColumns {
	return CalendarSummaryColumns{Row: b.Row(), Year: rows.NumberOf(b, &s.Year), Rows: rows.NumberOf(b, &s.Rows), Latest: rows.NullableTimeOf(b, &s.Latest)}
})
