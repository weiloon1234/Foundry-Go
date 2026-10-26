// Package intervalqueries verifies interval models, relations and query results.
package intervalqueries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Sample struct {
	ID          int
	Period      temporal.Interval
	MaybePeriod value.Nullable[temporal.Interval]
	Start       time.Time
	End         temporal.DateTime
	LocalStart  temporal.LocalDateTime
	LocalEnd    temporal.LocalDateTime
	ClockStart  temporal.Time
	ClockEnd    temporal.Time
	Date        temporal.Date
}

type Plan struct {
	ID          int
	Period      temporal.Interval
	Samples     relation.Many[Sample]
	Labels      relation.Through[Label, Link]
	SampleCount relation.Value[int64]
	TotalPeriod relation.Value[value.Nullable[temporal.Interval]]
}

type Label struct {
	ID   temporal.Interval
	Name string
}

type Link struct {
	ID         temporal.Interval
	PlanPeriod temporal.Interval
	LabelID    temporal.Interval
}

type Summary struct {
	Total   value.Nullable[temporal.Interval]
	Average value.Nullable[temporal.Interval]
	Count   int64
}
