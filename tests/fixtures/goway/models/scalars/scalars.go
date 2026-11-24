package scalars

import (
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=scalar_records var=Records key=ID identity=false
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
