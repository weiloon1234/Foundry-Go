package scalars

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Record struct {
	ID            int64
	Name          string
	MaybeName     value.Nullable[string]
	Date          temporal.Date
	MaybeDate     value.Nullable[temporal.Date]
	Local         temporal.LocalDateTime
	MaybeLocal    value.Nullable[temporal.LocalDateTime]
	Instant       temporal.DateTime
	MaybeInstant  value.Nullable[temporal.DateTime]
	Clock         temporal.Time
	MaybeClock    value.Nullable[temporal.Time]
	Period        temporal.Interval
	MaybePeriod   value.Nullable[temporal.Interval]
	Millis        int64
	MaybeMillis   value.Nullable[int64]
	Latitude      float64
	MaybeLatitude value.Nullable[float64]
}
type Table struct {
	orm.Table[Record, int64]
	Name          orm.Text[Record, string]
	MaybeName     orm.NullableText[Record, string]
	Date          orm.Time[Record, temporal.Date]
	MaybeDate     orm.NullableTime[Record, temporal.Date]
	Local         orm.Time[Record, temporal.LocalDateTime]
	MaybeLocal    orm.NullableTime[Record, temporal.LocalDateTime]
	Instant       orm.Time[Record, temporal.DateTime]
	MaybeInstant  orm.NullableTime[Record, temporal.DateTime]
	Clock         orm.Time[Record, temporal.Time]
	MaybeClock    orm.NullableTime[Record, temporal.Time]
	Period        orm.Interval[Record]
	MaybePeriod   orm.NullableInterval[Record]
	Millis        orm.Number[Record, int64]
	MaybeMillis   orm.NullableNumber[Record, int64]
	Latitude      orm.Number[Record, float64]
	MaybeLatitude orm.NullableNumber[Record, float64]
}

var Records = Declare()

func Declare() Table {
	return orm.Define("scalar_records", func(b orm.Builder[Record], r *Record) Table {
		return Table{Table: orm.TableOf(b, &r.ID),
			Name:          orm.TextOf(b, &r.Name),
			MaybeName:     orm.NullableTextOf(b, &r.MaybeName),
			Date:          orm.TimeOf(b, &r.Date),
			MaybeDate:     orm.NullableTimeOf(b, &r.MaybeDate),
			Local:         orm.TimeOf(b, &r.Local),
			MaybeLocal:    orm.NullableTimeOf(b, &r.MaybeLocal),
			Instant:       orm.TimeOf(b, &r.Instant),
			MaybeInstant:  orm.NullableTimeOf(b, &r.MaybeInstant),
			Clock:         orm.TimeOf(b, &r.Clock),
			MaybeClock:    orm.NullableTimeOf(b, &r.MaybeClock),
			Period:        orm.IntervalOf(b, &r.Period),
			MaybePeriod:   orm.NullableIntervalOf(b, &r.MaybePeriod),
			Millis:        orm.NumberOf(b, &r.Millis),
			MaybeMillis:   orm.NullableNumberOf(b, &r.MaybeMillis),
			Latitude:      orm.NumberOf(b, &r.Latitude),
			MaybeLatitude: orm.NullableNumberOf(b, &r.MaybeLatitude),
		}
	})
}
