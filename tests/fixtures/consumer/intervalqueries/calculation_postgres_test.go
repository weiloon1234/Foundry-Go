package intervalqueries_test

import (
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresIntervalCalculations(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, samples []intervalqueries.Sample) error {
		f := intervalqueries.Samples
		a, b := interval(t, -14, 3, -25*time.Hour-time.Microsecond), interval(t, 2, 1, time.Hour)
		input := expr.Param(f.Period, a)
		if got := intervalOne(t, tx, expr.AddIntervals(input, expr.Param(f.Period, b)).Value()); got != interval(t, -12, 4, -24*time.Hour-time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.SubtractIntervals(input, expr.Param(f.Period, b)).Value()); got != interval(t, -16, 2, -26*time.Hour-time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.NegateInterval(input).Value()); got != interval(t, 14, -3, 25*time.Hour+time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.IntervalMonths(input).Value()); got != -14 {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.IntervalDays(input).Value()); got != 3 {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.IntervalElapsed(input).Value()); got != -25*time.Hour-time.Microsecond {
			t.Fatal(got)
		}
		for _, expr := range []expr.Selected[intervalqueries.Sample, temporal.Interval]{expr.LocalDifference(expr.Value(f.LocalEnd), expr.Value(f.LocalStart)).Value(), expr.InstantDifference(expr.Value(f.End), expr.Value(f.Start)).Value()} {
			if got := intervalOne(t, tx, expr); got != interval(t, 0, 1, time.Hour) {
				t.Fatal(got)
			}
		}
		if got := intervalOne(t, tx, expr.ClockDifference(expr.Value(f.ClockEnd), expr.Value(f.ClockStart)).Value()); got != interval(t, 0, 0, -22*time.Hour) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.ShiftDate(expr.Value(f.Date), expr.Value(f.Period)).Value()).String(); got != "2024-02-29T00:00:00" {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.ShiftLocal(expr.Value(f.LocalStart), expr.Param(f.Period, temporal.Days(1))).Value()).String(); got != "2024-03-10T12:00:00" {
			t.Fatal(got)
		}
		ny, err := expr.LoadTimeZone("America/New_York")
		if err != nil {
			return err
		}
		for _, test := range []struct {
			period temporal.Interval
			hours  time.Duration
		}{{temporal.Days(1), 23}, {interval(t, 0, 0, 24*time.Hour), 24}} {
			got := intervalOne(t, tx, expr.ShiftInstant(expr.Value(f.Start), expr.Param(f.Period, test.period), ny).Value())
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
		wide := expr.LocalDifference(expr.Param(f.LocalEnd, late), expr.Param(f.LocalStart, early))
		if got := intervalOne(t, tx, wide.Value()); got != interval(t, 0, 3652058, 24*time.Hour-time.Microsecond) {
			t.Fatal("wide difference lost days", got)
		}
		if got := intervalOne(t, tx, expr.ShiftLocal(expr.Param(f.LocalStart, early), wide).Value()); got != late {
			t.Fatal(got)
		}
		return nil
	})
}

func TestPostgresIntervalSelectedAndNullable(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, samples []intervalqueries.Sample) error {
		q := intervalqueries.Samples.Query()
		f := intervalqueries.Samples
		schema := intervalqueries.Summaries
		selected := rows.Read(rows.Select(rows.From(q.Where(orm.NumberKey(f.ID).Lt(4))), schema,
			rows.MapSelected(schema.Total, expr.SumInterval(f.Period).Value()),
			rows.MapSelected(schema.Average, expr.AvgInterval(f.Period).Value()),
			rows.MapSelected(schema.Count, expr.Count[intervalqueries.Sample]().Value())))
		summary, err := selected.RequireFirst(t.Context(), tx)
		if err != nil || summary.Total != value.Of(interval(t, 1, 30, 720*time.Hour)) || summary.Average != value.Of(interval(t, 0, 20, 240*time.Hour)) || summary.Count != 3 {
			t.Fatal(summary, err)
		}
		empty, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.Where(orm.NumberKey(f.ID).Lt(0))), expr.SumInterval(f.Period).Value())).RequireFirst(t.Context(), tx)
		if err != nil || !empty.IsNull() {
			t.Fatal(empty, err)
		}
		window := expr.WindowFor(rows.ModelScope(q))
		shifted := expr.ShiftDateNullableValue(expr.Nullable(expr.Row(expr.Value(f.Date)).Value()), expr.Over(expr.AvgInterval(f.Period).Filter(expr.ModelPredicate(orm.NumberKey(f.ID).Lt(4))), window))
		if got := intervalOne(t, tx, shifted); got != value.Of(mustLocal(t, "2024-03-01T00:00:00")) {
			t.Fatal(got)
		}
		values, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.OrderBy(f.ID.Asc())), expr.AddIntervalsNullable(expr.Value(f.MaybePeriod), expr.NullableRow(expr.Param(f.Period, temporal.Interval{}))).Value())).All(t.Context(), tx)
		if err != nil || len(values) != len(samples) {
			t.Fatal(values, err)
		}
		for i, s := range samples {
			if values[i] != s.MaybePeriod {
				t.Fatal(i, values[i], s.MaybePeriod)
			}
		}
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.IntervalMonthsNullableValue(expr.SumInterval(f.Period).Value()))).RequireFirst(t.Context(), tx); err != nil || got != value.Of(int32(1)) {
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
