// Package temporalqueries exercises typed SQL date, time and instant operations.
package temporalqueries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=temporal_samples primary=ID
type Sample struct {
	ID         int
	Millis     int64
	At         time.Time
	Instant    temporal.DateTime
	Date       temporal.Date
	Local      temporal.LocalDateTime
	Clock      temporal.Time
	MaybeAt    value.Nullable[time.Time]
	MaybeDate  value.Nullable[temporal.Date]
	MaybeLocal value.Nullable[temporal.LocalDateTime]
	MaybeClock value.Nullable[temporal.Time]
}

//foundry:projection
type CalendarSummary struct {
	Year   int64
	Rows   int64
	Latest value.Nullable[temporal.Date]
}
