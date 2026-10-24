package intervalqueries_test

import (
	"slices"
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresIntervalRelationEquality(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, samples []intervalqueries.Sample) error {
		for i, period := range []temporal.Interval{temporal.Months(1), temporal.Days(30), temporal.Days(2)} {
			if _, err := intervalqueries.Plans.Insert(t.Context(), tx, intervalqueries.Plans.ID.Set(i+1), intervalqueries.Plans.Period.Set(period)); err != nil {
				return err
			}
		}
		labels := intervalqueries.Labels
		lf := intervalqueries.Labels
		if _, err := labels.Insert(t.Context(), tx, lf.ID.Set(temporal.Months(1)), lf.Name.Set("month")); err != nil {
			return err
		}
		if _, err := intervalqueries.Links.Insert(t.Context(), tx, intervalqueries.Links.ID.Set(temporal.Months(2)), intervalqueries.Links.PlanPeriod.Set(temporal.Days(30)), intervalqueries.Links.LabelID.Set(interval(t, 0, 0, 720*time.Hour))); err != nil {
			return err
		}
		rel, agg := intervalqueries.PlanLinks, intervalqueries.PlanValues
		parents, err := intervalqueries.Plans.Query().OrderBy(intervalqueries.Plans.ID.Asc()).With(rel.Samples.OrderBy(intervalqueries.Samples.ID.Asc()), rel.Labels, agg.SampleCount, agg.TotalPeriod).All(t.Context(), tx)
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
		found, err := labels.Require(t.Context(), tx, temporal.Days(30))
		if err != nil || found.ID != temporal.Months(1) {
			t.Fatal(found, err)
		}
		updated, err := orm.Upsert(t.Context(), tx, labels.Query(), orm.OnConflict(orm.ConflictKey(lf.ID)).DoUpdate(orm.ConflictIncoming(lf.Name)), lf.ID.Set(temporal.Days(30)), lf.Name.Set("changed"))
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
		q := intervalqueries.Samples.Query().OrderBy(intervalqueries.Samples.Period.Asc())
		request := orm.CursorRequest[intervalqueries.Sample]{Size: 1}
		var ids []int
		for step := 0; step < 5; step++ {
			page, err := orm.CursorPaginate(t.Context(), tx, q, request)
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
			parsed, err := orm.ParseCursor[intervalqueries.Sample](next.Token())
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
