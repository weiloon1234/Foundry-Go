package intervalqueries_test

import (
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresIntervalCalculations(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, samples []intervalqueries.Sample) error {
		f := intervalqueries.SampleFields()
		a, b := interval(t, -14, 3, -25*time.Hour-time.Microsecond), interval(t, 2, 1, time.Hour)
		input := query.Param(f.Period, a)
		if got := intervalOne(t, tx, query.AddIntervals(input, query.Param(f.Period, b)).Value()); got != interval(t, -12, 4, -24*time.Hour-time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, query.SubtractIntervals(input, query.Param(f.Period, b)).Value()); got != interval(t, -16, 2, -26*time.Hour-time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, query.NegateInterval(input).Value()); got != interval(t, 14, -3, 25*time.Hour+time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, query.IntervalMonths(input).Value()); got != -14 {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, query.IntervalDays(input).Value()); got != 3 {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, query.IntervalElapsed(input).Value()); got != -25*time.Hour-time.Microsecond {
			t.Fatal(got)
		}
		for _, expr := range []query.Expression[intervalqueries.Sample, temporal.Interval]{query.LocalDifference(f.LocalEnd, f.LocalStart).Value(), query.InstantDifference(f.End, f.Start).Value()} {
			if got := intervalOne(t, tx, expr); got != interval(t, 0, 1, time.Hour) {
				t.Fatal(got)
			}
		}
		if got := intervalOne(t, tx, query.ClockDifference(f.ClockEnd, f.ClockStart).Value()); got != interval(t, 0, 0, -22*time.Hour) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, query.ShiftDate(f.Date, f.Period).Value()).String(); got != "2024-02-29T00:00:00" {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, query.ShiftLocal(f.LocalStart, query.Param(f.Period, temporal.Days(1))).Value()).String(); got != "2024-03-10T12:00:00" {
			t.Fatal(got)
		}
		ny, err := query.LoadTimeZone("America/New_York")
		if err != nil {
			return err
		}
		for _, test := range []struct {
			period temporal.Interval
			hours  time.Duration
		}{{temporal.Days(1), 23}, {interval(t, 0, 0, 24*time.Hour), 24}} {
			got := intervalOne(t, tx, query.ShiftInstant(f.Start, query.Param(f.Period, test.period), ny).Value())
			if got.Sub(samples[0].Start) != test.hours*time.Hour {
				t.Fatal(got, test.hours)
			}
		}
		early, err := temporal.ParseLocalDateTime("0001-01-01T00:00:00")
		if err != nil {
			return err
		}
		late, err := temporal.ParseLocalDateTime("9999-12-31T23:59:59.999999")
		if err != nil {
			return err
		}
		wide := query.LocalDifference(query.Param(f.LocalEnd, late), query.Param(f.LocalStart, early))
		if got := intervalOne(t, tx, wide.Value()); got != interval(t, 0, 3652058, 24*time.Hour-time.Microsecond) {
			t.Fatal("wide difference lost days", got)
		}
		if got := intervalOne(t, tx, query.ShiftLocal(query.Param(f.LocalStart, early), wide).Value()); got != late {
			t.Fatal(got)
		}
		return nil
	})
}

func TestPostgresIntervalSelectedAndNullable(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, samples []intervalqueries.Sample) error {
		q := intervalqueries.QueryIntervalSamples()
		f := intervalqueries.SampleFields()
		selected := intervalqueries.ProjectSummary(q.Where(f.ID.Lt(4))).SelectTotal(query.Sum(f.Period).Value()).SelectAverage(query.Avg(f.Period).Value()).SelectCount(query.Count[intervalqueries.Sample]().Value()).Query()
		summary, err := selected.RequireFirst(t.Context(), tx)
		if err != nil || summary.Total != value.Of(interval(t, 1, 30, 720*time.Hour)) || summary.Average != value.Of(interval(t, 0, 20, 240*time.Hour)) || summary.Count != 3 {
			t.Fatal(summary, err)
		}
		empty, err := query.SelectValue(q.Where(f.ID.Lt(0)), query.Sum(f.Period).Value()).RequireFirst(t.Context(), tx)
		if err != nil || !empty.IsNull() {
			t.Fatal(empty, err)
		}
		window := query.WindowFor(q)
		shifted := query.ShiftDateNullableValue(query.Nullable(query.Value(f.Date)), query.Avg(f.Period).Filter(f.ID.Lt(4)).Over(window))
		if got := intervalOne(t, tx, shifted); got != value.Of(mustLocal(t, "2024-03-01T00:00:00")) {
			t.Fatal(got)
		}
		values, err := query.SelectValue(q.OrderBy(f.ID.Asc()), query.AddIntervalsNullable(f.MaybePeriod, query.NullableRow(query.Param(f.Period, temporal.Interval{}))).Value()).All(t.Context(), tx)
		if err != nil || len(values) != len(samples) {
			t.Fatal(values, err)
		}
		for i, s := range samples {
			if values[i] != s.MaybePeriod {
				t.Fatal(i, values[i], s.MaybePeriod)
			}
		}
		if got, err := query.SelectValue(q, query.IntervalMonthsNullableValue(query.Sum(f.Period).Value())).RequireFirst(t.Context(), tx); err != nil || got != value.Of(int32(1)) {
			t.Fatal(got, err)
		}
		return nil
	})
}

func mustLocal(t *testing.T, text string) temporal.LocalDateTime {
	t.Helper()
	v, err := temporal.ParseLocalDateTime(text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
