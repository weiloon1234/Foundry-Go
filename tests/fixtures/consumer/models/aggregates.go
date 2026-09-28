package models

import "github.com/weiloon1234/Foundry-Go/database/query"

// DefineAggregates owns domain computations; generation binds concrete slots.
func (User) DefineAggregates() UserAggregateSet {
	r := UserRelations()
	return UserAggregateSet{
		OrderCount:         query.Related(r.Orders, query.Count[Order]()),
		OrderTotal:         query.Related(r.Orders, OrderFields().TotalCents.Sum()),
		OrderAverage:       query.Related(r.Orders, OrderFields().TotalCents.Avg()),
		OrderMinimum:       query.Related(r.Orders, OrderFields().TotalCents.Min()),
		OrderMaximum:       query.Related(r.Orders, OrderFields().TotalCents.Max()),
		GroupCount:         query.Related(r.Groups, query.Count[Group]()),
		GroupDistinct:      query.Related(r.Groups, GroupFields().Code.CountDistinct()),
		GroupPriority:      query.Related(r.Groups.Pivot(), MembershipFields().Priority.Sum()),
		HasGroups:          query.Related(r.Groups, query.Exists[Group]()),
		MeasurementTotal:   query.Related(r.Measurements, MeasurementFields().Amount.Sum()),
		MeasurementAverage: query.Related(r.Measurements, MeasurementFields().Amount.Avg()),
		MeasurementScore:   query.Related(r.Measurements, MeasurementFields().Score.Avg()),
		MeasurementCount:   query.Related(r.Measurements, MeasurementFields().Amount.Count()),
		FirstLabel:         query.Related(r.Measurements, MeasurementFields().Label.Min()),
	}
}
