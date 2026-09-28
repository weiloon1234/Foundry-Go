// Package intervalqueries verifies interval models, relations and query results.
package intervalqueries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=interval_samples primary=ID
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

//foundry:model table=interval_plans primary=ID
type Plan struct {
	ID          int
	Period      temporal.Interval
	Samples     relation.Many[Sample]
	Labels      relation.Through[Label, Link]
	SampleCount relation.Value[int64]
	TotalPeriod relation.Value[value.Nullable[temporal.Interval]]
}

func (Plan) DefineRelations() PlanRelationSet {
	p, s, l, label := PlanFields(), SampleFields(), LinkFields(), LabelFields()
	return PlanRelationSet{
		Samples: query.HasMany(p.Period, s.Period),
		Labels:  query.ManyToMany(p.Period, l.PlanPeriod, l.LabelID, label.ID),
	}
}
func (Plan) DefineAggregates() PlanAggregateSet {
	r := PlanRelations()
	return PlanAggregateSet{SampleCount: query.Related(r.Samples, query.Count[Sample]()), TotalPeriod: query.Related(r.Samples, SampleFields().Period.Sum())}
}

//foundry:model table=interval_labels primary=ID
type Label struct {
	ID   temporal.Interval
	Name string
}

//foundry:model table=interval_links primary=ID
type Link struct {
	ID         temporal.Interval
	PlanPeriod temporal.Interval
	LabelID    temporal.Interval
}

//foundry:projection
type Summary struct {
	Total   value.Nullable[temporal.Interval]
	Average value.Nullable[temporal.Interval]
	Count   int64
}
