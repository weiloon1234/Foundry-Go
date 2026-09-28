package intervalqueries_test

import (
	"slices"
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresIntervalRelationEquality(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, samples []intervalqueries.Sample) error {
		for i, period := range []temporal.Interval{temporal.Months(1), temporal.Days(30), temporal.Days(2)} {
			if _, err := intervalqueries.QueryIntervalPlans().Create(t.Context(), tx, intervalqueries.PlanDraft{}.SetID(i+1).SetPeriod(period)); err != nil {
				return err
			}
		}
		labels := intervalqueries.QueryIntervalLabels()
		lf := intervalqueries.LabelFields()
		if _, err := labels.Create(t.Context(), tx, intervalqueries.LabelDraft{}.SetID(temporal.Months(1)).SetName("month")); err != nil {
			return err
		}
		if _, err := intervalqueries.QueryIntervalLinks().Create(t.Context(), tx, intervalqueries.LinkDraft{}.SetID(temporal.Months(2)).SetPlanPeriod(temporal.Days(30)).SetLabelID(interval(t, 0, 0, 720*time.Hour))); err != nil {
			return err
		}
		rel, agg := intervalqueries.PlanRelations(), intervalqueries.PlanAggregates()
		parents, err := intervalqueries.QueryIntervalPlans().OrderBy(intervalqueries.PlanFields().ID.Asc()).With(rel.Samples.OrderBy(intervalqueries.SampleFields().ID.Asc()), rel.Labels, agg.SampleCount, agg.TotalPeriod).All(t.Context(), tx)
		if err != nil || len(parents) != 3 {
			t.Fatal(parents, err)
		}
		for i, p := range parents[:2] {
			children, loaded := p.Samples.Get()
			count, countLoaded := p.SampleCount.Get()
			sum, sumLoaded := p.TotalPeriod.Get()
			links, linksLoaded := p.Labels.Get()
			if !loaded || !countLoaded || !sumLoaded || !linksLoaded || len(children) != 3 || count != 3 || sum != value.Of(interval(t, 1, 30, 720*time.Hour)) || len(links) != 1 {
				t.Fatal(i, p)
			}
			for j, s := range children {
				if s.Period != samples[j].Period {
					t.Fatal("hydration normalized stored components", s.Period)
				}
			}
			if links[0].Model.ID != temporal.Months(1) || links[0].Pivot.ID != temporal.Months(2) || links[0].Pivot.PlanPeriod != temporal.Days(30) {
				t.Fatal(links)
			}
		}
		if parents[0].Period != temporal.Months(1) || parents[1].Period != temporal.Days(30) {
			t.Fatal("parent components changed")
		}
		empty, loaded := parents[2].Samples.Get()
		sum, sumLoaded := parents[2].TotalPeriod.Get()
		if !loaded || len(empty) != 0 || !sumLoaded || !sum.IsNull() {
			t.Fatal(parents[2])
		}
		found, err := labels.RequireFind(t.Context(), tx, temporal.Days(30))
		if err != nil || found.ID != temporal.Months(1) {
			t.Fatal(found, err)
		}
		updated, err := labels.Upsert(t.Context(), tx, intervalqueries.LabelDraft{}.SetID(temporal.Days(30)).SetName("changed"), query.OnConflict(lf.ID).DoUpdate(lf.Name.Incoming()))
		record, ok := updated.Get()
		if err != nil || !ok || record.ID != temporal.Months(1) || record.Name != "changed" {
			t.Fatal(record, err)
		}
		removed, err := labels.Delete(t.Context(), tx, temporal.Days(30))
		if err != nil || removed.ID != temporal.Months(1) {
			t.Fatal(removed, err)
		}
		return nil
	})
}

func TestPostgresIntervalCursorEquality(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, _ []intervalqueries.Sample) error {
		q := intervalqueries.QueryIntervalSamples().OrderBy(intervalqueries.SampleFields().Period.Asc())
		request := query.CursorRequest[intervalqueries.Sample]{Size: 1}
		var ids []int
		for step := 0; step < 5; step++ {
			page, err := q.CursorPaginate(t.Context(), tx, request)
			if err != nil {
				return err
			}
			for _, s := range page.Items {
				ids = append(ids, s.ID)
			}
			next, set := page.Next.Get()
			if !set {
				break
			}
			parsed, err := query.ParseCursor[intervalqueries.Sample](next.Token())
			if err != nil {
				return err
			}
			request.After = value.Set(parsed)
		}
		if !slices.Equal(ids, []int{4, 1, 2, 3}) {
			t.Fatal("SQL-equivalent interval ties skipped or repeated", ids)
		}
		return nil
	})
}
