package intervalqueries

import (
	"time"

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

// doc-example:begin interval-queries-2
func ShiftedStart(period temporal.Interval, zoneName string) (expr.Selected[Sample, time.Time], error) {
	zone, err := expr.LoadTimeZone(zoneName)
	if err != nil {
		return expr.Selected[Sample, time.Time]{}, err
	}
	return expr.AsSelected(expr.ShiftInstant(expr.Value(Samples.Start), expr.Param(Samples.Period, period), zone)), nil
}

// doc-example:end interval-queries-2

// doc-example:begin interval-queries-3
func PeriodSummary() rows.Query[Sample, Summary] {
	return rows.Select(rows.From(Samples.Query()), Summaries,
		rows.MapSelected(Summaries.Total, expr.AsSelected(expr.SumInterval(Samples.Period))),
		rows.MapSelected(Summaries.Average, expr.AsSelected(expr.AvgInterval(Samples.Period))),
		rows.MapSelected(Summaries.Count, expr.AsSelected(expr.Count[Sample]())))
}

// doc-example:end interval-queries-3
