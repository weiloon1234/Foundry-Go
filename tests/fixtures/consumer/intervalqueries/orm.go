package intervalqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var PlanLinks = struct {
	Samples orm.HasMany[Plan, Sample]
	Labels  orm.ThroughRelation[Plan, Label, Link]
}{
	Samples: orm.HasManyOf(Plans.Samples, Plans.Period, Samples.Period),
	Labels:  orm.ManyToManyOf(Plans.Labels, orm.JoinKeys(Plans.Period, Links.PlanPeriod), orm.JoinKeys(Links.LabelID, Labels.ID)),
}

var PlanValues = struct {
	SampleCount orm.AggregateRelation[Plan, int64]
	TotalPeriod orm.AggregateRelation[Plan, value.Nullable[temporal.Interval]]
}{
	SampleCount: Plans.SampleCount.Using(expr.Related(PlanLinks.Samples, expr.Count[Sample]())),
	TotalPeriod: Plans.TotalPeriod.Using(expr.Related(PlanLinks.Samples, expr.SumInterval(Samples.Period))),
}

type SummaryColumns struct {
	rows.Row[Summary]
	Total   rows.Field[Summary, value.Nullable[temporal.Interval]]
	Average rows.Field[Summary, value.Nullable[temporal.Interval]]
	Count   rows.Field[Summary, int64]
}

var Summaries = rows.Define(func(b rows.Builder[Summary], s *Summary) SummaryColumns {
	return SummaryColumns{Row: b.Row(), Total: rows.NullableFieldOf(b, &s.Total, orm.Codec(intervalCodec())), Average: rows.NullableFieldOf(b, &s.Average, orm.Codec(intervalCodec())), Count: rows.NumberOf(b, &s.Count)}
})

func intervalCodec() codec.Codec[temporal.Interval] { return codec.Interval() }
