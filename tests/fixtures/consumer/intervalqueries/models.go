// Package intervalqueries verifies interval models, relations and query results.
package intervalqueries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin interval-queries
//
//foundry:model table=interval_samples var=Samples key=ID identity=false
type Sample struct {
	ID          int `foundry:"member=ID,key-role=lookup"`
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

// doc-example:end interval-queries

//foundry:model table=interval_plans var=Plans key=ID identity=false
type Plan struct {
	ID          int `foundry:"member=ID,key-role=lookup"`
	Period      temporal.Interval
	Samples     relation.Many[Sample]
	Labels      relation.Through[Label, Link]
	SampleCount relation.Value[int64]
	TotalPeriod relation.Value[value.Nullable[temporal.Interval]]
}

//foundry:model table=interval_labels var=Labels key=ID identity=false
type Label struct {
	ID   temporal.Interval `foundry:"codec=intervalCodec,member=ID"`
	Name string
}

//foundry:model table=interval_links var=Links key=ID identity=false
type Link struct {
	ID         temporal.Interval `foundry:"codec=intervalCodec,member=ID"`
	PlanPeriod temporal.Interval
	LabelID    temporal.Interval
}

type Summary struct {
	Total   value.Nullable[temporal.Interval]
	Average value.Nullable[temporal.Interval]
	Count   int64
}
