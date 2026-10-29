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

type SampleColumns struct {
	orm.Table[Sample, int]
	ID          orm.Key[Sample, int]
	Period      orm.Interval[Sample]
	MaybePeriod orm.NullableInterval[Sample]
	Start       orm.Time[Sample, time.Time]
	End         orm.Time[Sample, temporal.DateTime]
	LocalStart  orm.Time[Sample, temporal.LocalDateTime]
	LocalEnd    orm.Time[Sample, temporal.LocalDateTime]
	ClockStart  orm.Time[Sample, temporal.Time]
	ClockEnd    orm.Time[Sample, temporal.Time]
	Date        orm.Time[Sample, temporal.Date]
}

var Samples = orm.Define("interval_samples", func(b orm.Builder[Sample], s *Sample) SampleColumns {
	table := orm.TableOf(b, &s.ID)
	return SampleColumns{
		Table: table, ID: table.Key(), Period: orm.IntervalOf(b, &s.Period), MaybePeriod: orm.NullableIntervalOf(b, &s.MaybePeriod),
		Start: orm.TimeOf(b, &s.Start), End: orm.TimeOf(b, &s.End), LocalStart: orm.TimeOf(b, &s.LocalStart), LocalEnd: orm.TimeOf(b, &s.LocalEnd),
		ClockStart: orm.TimeOf(b, &s.ClockStart), ClockEnd: orm.TimeOf(b, &s.ClockEnd), Date: orm.TimeOf(b, &s.Date),
	}
})

type PlanColumns struct {
	orm.Table[Plan, int]
	ID          orm.Key[Plan, int]
	Period      orm.Interval[Plan]
	Samples     orm.Many[Plan, Sample]
	Labels      orm.Through[Plan, Label, Link]
	SampleCount orm.Value[Plan, int64]
	TotalPeriod orm.Value[Plan, value.Nullable[temporal.Interval]]
}

var Plans = orm.Define("interval_plans", func(b orm.Builder[Plan], p *Plan) PlanColumns {
	table := orm.TableOf(b, &p.ID)
	return PlanColumns{Table: table, ID: table.Key(), Period: orm.IntervalOf(b, &p.Period), Samples: orm.ManyOf(b, &p.Samples), Labels: orm.ThroughOf(b, &p.Labels), SampleCount: orm.ValueOf(b, &p.SampleCount), TotalPeriod: orm.ValueOf(b, &p.TotalPeriod)}
})

type LabelColumns struct {
	orm.Table[Label, temporal.Interval]
	ID   orm.Key[Label, temporal.Interval]
	Name orm.Text[Label, string]
}

var Labels = orm.Define("interval_labels", func(b orm.Builder[Label], l *Label) LabelColumns {
	table := orm.TableOf(b, &l.ID, orm.Codec(codec.Interval()))
	return LabelColumns{Table: table, ID: table.Key(), Name: orm.TextOf(b, &l.Name)}
})

type LinkColumns struct {
	orm.Table[Link, temporal.Interval]
	ID         orm.Key[Link, temporal.Interval]
	PlanPeriod orm.Interval[Link]
	LabelID    orm.Interval[Link]
}

var Links = orm.Define("interval_links", func(b orm.Builder[Link], l *Link) LinkColumns {
	table := orm.TableOf(b, &l.ID, orm.Codec(codec.Interval()))
	return LinkColumns{Table: table, ID: table.Key(), PlanPeriod: orm.IntervalOf(b, &l.PlanPeriod), LabelID: orm.IntervalOf(b, &l.LabelID)}
})

var PlanLinks = struct {
	Samples orm.HasMany[Plan, Sample]
	Labels  orm.ThroughRelation[Plan, Label, Link]
}{
	Samples: orm.HasManyOf(Plans.Samples, orm.IntervalExpr(Plans.Period), orm.IntervalExpr(Samples.Period)),
	Labels:  orm.ManyToManyOf(Plans.Labels, orm.IntervalExpr(Plans.Period), orm.IntervalExpr(Links.PlanPeriod), orm.IntervalExpr(Links.LabelID), orm.KeyExpr(Labels.ID)),
}

var PlanValues = struct {
	SampleCount orm.AggregateRelation[Plan, int64]
	TotalPeriod orm.AggregateRelation[Plan, value.Nullable[temporal.Interval]]
}{
	SampleCount: Plans.SampleCount.Using(expr.Related(PlanLinks.Samples, expr.Count[Sample]())),
	TotalPeriod: Plans.TotalPeriod.Using(expr.Related(PlanLinks.Samples, expr.SumInterval(orm.IntervalExpr(Samples.Period)))),
}

type SummaryColumns struct {
	rows.Row[Summary]
	Total   rows.Field[Summary, value.Nullable[temporal.Interval]]
	Average rows.Field[Summary, value.Nullable[temporal.Interval]]
	Count   rows.Field[Summary, int64]
}

var Summaries = rows.Define(func(b rows.Builder[Summary], s *Summary) SummaryColumns {
	return SummaryColumns{Row: b.Row(), Total: rows.NullableFieldOf(b, &s.Total, orm.Codec(codec.Interval())), Average: rows.NullableFieldOf(b, &s.Average, orm.Codec(codec.Interval())), Count: rows.NumberOf(b, &s.Count)}
})
