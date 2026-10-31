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
		input := expr.Param(orm.IntervalExpr(f.Period), a)
		if got := intervalOne(t, tx, expr.AsSelected(expr.AddIntervals(input, expr.Param(orm.IntervalExpr(f.Period), b)))); got != interval(t, -12, 4, -24*time.Hour-time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.SubtractIntervals(input, expr.Param(orm.IntervalExpr(f.Period), b)))); got != interval(t, -16, 2, -26*time.Hour-time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.NegateInterval(input))); got != interval(t, 14, -3, 25*time.Hour+time.Microsecond) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.IntervalMonths(input))); got != -14 {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.IntervalDays(input))); got != 3 {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.IntervalElapsed(input))); got != -25*time.Hour-time.Microsecond {
			t.Fatal(got)
		}
		for _, expr := range []expr.Selected[intervalqueries.Sample, temporal.Interval]{expr.AsSelected(expr.LocalDifference(expr.Value(orm.TimeExpr(f.LocalEnd)), expr.Value(orm.TimeExpr(f.LocalStart)))), expr.AsSelected(expr.InstantDifference(expr.Value(orm.TimeExpr(f.End)), expr.Value(orm.TimeExpr(f.Start))))} {
			if got := intervalOne(t, tx, expr); got != interval(t, 0, 1, time.Hour) {
				t.Fatal(got)
			}
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.ClockDifference(expr.Value(orm.TimeExpr(f.ClockEnd)), expr.Value(orm.TimeExpr(f.ClockStart))))); got != interval(t, 0, 0, -22*time.Hour) {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.ShiftDate(expr.Value(orm.TimeExpr(f.Date)), expr.Value(orm.IntervalExpr(f.Period))))).String(); got != "2024-02-29T00:00:00" {
			t.Fatal(got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.ShiftLocal(expr.Value(orm.TimeExpr(f.LocalStart)), expr.Param(orm.IntervalExpr(f.Period), temporal.Days(1))))).String(); got != "2024-03-10T12:00:00" {
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
			got := intervalOne(t, tx, expr.AsSelected(expr.ShiftInstant(expr.Value(orm.TimeExpr(f.Start)), expr.Param(orm.IntervalExpr(f.Period), test.period), ny)))
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
		wide := expr.LocalDifference(expr.Param(orm.TimeExpr(f.LocalEnd), late), expr.Param(orm.TimeExpr(f.LocalStart), early))
		if got := intervalOne(t, tx, expr.AsSelected(wide)); got != interval(t, 0, 3652058, 24*time.Hour-time.Microsecond) {
			t.Fatal("wide difference lost days", got)
		}
		if got := intervalOne(t, tx, expr.AsSelected(expr.ShiftLocal(expr.Param(orm.TimeExpr(f.LocalStart), early), wide))); got != late {
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
		selected := rows.Read(rows.Select(rows.From(q.Where(orm.NumberKeyLt(orm.NumberKey(f.ID), 4))), schema,
			rows.MapSelected(schema.Total, expr.AsSelected(expr.SumInterval(orm.IntervalExpr(f.Period)))),
			rows.MapSelected(schema.Average, expr.AsSelected(expr.AvgInterval(orm.IntervalExpr(f.Period)))),
			rows.MapSelected(schema.Count, expr.AsSelected(expr.Count[intervalqueries.Sample]()))))
		summary, err := selected.RequireFirst(t.Context(), tx)
		if err != nil || summary.Total != value.Of(interval(t, 1, 30, 720*time.Hour)) || summary.Average != value.Of(interval(t, 0, 20, 240*time.Hour)) || summary.Count != 3 {
			t.Fatal(summary, err)
		}
		empty, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.Where(orm.NumberKeyLt(orm.NumberKey(f.ID), 0))), expr.AsSelected(expr.SumInterval(orm.IntervalExpr(f.Period))))).RequireFirst(t.Context(), tx)
		if err != nil || !empty.IsNull() {
			t.Fatal(empty, err)
		}
		window := expr.WindowFor(rows.ModelScope(q))
		shifted := expr.ShiftDateNullableValue(expr.Nullable(expr.AsSelected(expr.Row(expr.Value(orm.TimeExpr(f.Date))))), expr.Over(expr.Filter(expr.AvgInterval(orm.IntervalExpr(f.Period)), expr.ModelPredicate(orm.NumberKeyLt(orm.NumberKey(f.ID), 4))), window))
		if got := intervalOne(t, tx, shifted); got != value.Of(mustLocal(t, "2024-03-01T00:00:00")) {
			t.Fatal(got)
		}
		values, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.OrderBy(orm.KeyAsc(f.ID))), expr.AsSelected(expr.AddIntervalsNullable(expr.Value(orm.NullableIntervalExpr(f.MaybePeriod)), expr.NullableRow(expr.Param(orm.IntervalExpr(f.Period), temporal.Interval{})))))).All(t.Context(), tx)
		if err != nil || len(values) != len(samples) {
			t.Fatal(values, err)
		}
		for i, s := range samples {
			if values[i] != s.MaybePeriod {
				t.Fatal(i, values[i], s.MaybePeriod)
			}
		}
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.IntervalMonthsNullableValue(expr.AsSelected(expr.SumInterval(orm.IntervalExpr(f.Period)))))).RequireFirst(t.Context(), tx); err != nil || got != value.Of(int32(1)) {
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
