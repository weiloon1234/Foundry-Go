package models

import "github.com/weiloon1234/Foundry-Go/database/query"

// DefineAggregates owns domain computations; generation binds concrete slots.
func (User) DefineAggregates() UserAggregateSet {
	r := UserRelations()
	return UserAggregateSet{
		OrderCount:         query.Related(r.Orders, query.Count[Order]()),
		OrderTotal:         query.Related(r.Orders, query.Sum(OrderFields().TotalCents)),
		OrderAverage:       query.Related(r.Orders, query.Avg(OrderFields().TotalCents)),
		OrderMinimum:       query.Related(r.Orders, query.Min(OrderFields().TotalCents)),
		OrderMaximum:       query.Related(r.Orders, query.Max(OrderFields().TotalCents)),
		GroupCount:         query.Related(r.Groups, query.Count[Group]()),
		GroupDistinct:      query.Related(r.Groups, query.CountDistinctField(GroupFields().Code)),
		GroupPriority:      query.Related(r.Groups.Pivot(), query.Sum(MembershipFields().Priority)),
		HasGroups:          query.Related(r.Groups, query.Exists[Group]()),
		MeasurementTotal:   query.Related(r.Measurements, query.Sum(MeasurementFields().Amount)),
		MeasurementAverage: query.Related(r.Measurements, query.Avg(MeasurementFields().Amount)),
		MeasurementScore:   query.Related(r.Measurements, query.Avg(MeasurementFields().Score)),
		MeasurementCount:   query.Related(r.Measurements, query.CountField(MeasurementFields().Amount)),
		FirstLabel:         query.Related(r.Measurements, query.Min(MeasurementFields().Label)),
	}
}
