package temporalqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"
	"time"

	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresTemporalTruncationAndZones(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		f := temporalqueries.Samples
		ny, err := expr.LoadTimeZone("America/New_York")
		if err != nil {
			return err
		}
		for _, test := range []struct {
			unit expr.DateUnit
			want string
		}{
			{expr.DateDay, "2024-03-09"}, {expr.DateWeek, "2024-03-04"}, {expr.DateMonth, "2024-03-01"},
			{expr.DateQuarter, "2024-01-01"}, {expr.DateYear, "2024-01-01"}, {expr.DateDecade, "2020-01-01"},
			{expr.DateCentury, "2001-01-01"}, {expr.DateMillennium, "2001-01-01"},
		} {
			if got := temporalOne(t, tx, expr.AsSelected(expr.TruncateDate(expr.Param(orm.TimeExpr(f.Date), samples[0].Date), test.unit))).String(); got != test.want {
				t.Fatal(test.unit, got, test.want)
			}
		}
		for _, test := range []struct {
			unit expr.TimestampUnit
			want string
		}{
			{expr.TimestampMicrosecond, "2024-03-09T12:34:56.123456"},
			{expr.TimestampMillisecond, "2024-03-09T12:34:56.123"},
			{expr.TimestampSecond, "2024-03-09T12:34:56"},
			{expr.TimestampMinute, "2024-03-09T12:34:00"},
			{expr.TimestampHour, "2024-03-09T12:00:00"},
			{expr.TimestampDay, "2024-03-09T00:00:00"},
			{expr.TimestampWeek, "2024-03-04T00:00:00"},
		} {
			want, err := temporal.ParseLocalDateTime(test.want)
			if err != nil {
				return err
			}
			if got := temporalOne(t, tx, expr.AsSelected(expr.TruncateLocal(expr.Param(orm.TimeExpr(f.Local), samples[0].Local), test.unit))); got != want {
				t.Fatal(test.unit, got, want)
			}
		}
		at := expr.Param(orm.TimeExpr(f.At), samples[0].At)
		for _, test := range []struct {
			zone expr.TimeZone
			hour int
		}{{ny, 5}, {expr.UTCZone(), 0}} {
			got := temporalOne(t, tx, expr.AsSelected(expr.TruncateInstant(at, expr.TimestampDay, test.zone)))
			if !got.Equal(time.Date(2024, 3, 9, test.hour, 0, 0, 0, time.UTC)) {
				t.Fatal(test.zone, got)
			}
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.LocalAt(at, ny))); got != samples[0].Local {
			t.Fatal(got, samples[0].Local)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.LocalAt(expr.Param(orm.TimeExpr(f.Instant), samples[0].Instant), ny))); got != samples[0].Local {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.TruncateInstant(expr.Param(orm.TimeExpr(f.Instant), samples[0].Instant), expr.TimestampDay, ny))); !got.UTC().Equal(time.Date(2024, 3, 9, 5, 0, 0, 0, time.UTC)) {
			t.Fatal(got)
		}
		for _, test := range []struct{ local, instant string }{
			{"2024-03-10T02:30:00", "2024-03-10T07:30:00Z"},
			{"2024-11-03T01:30:00", "2024-11-03T06:30:00Z"},
		} {
			local, err := temporal.ParseLocalDateTime(test.local)
			if err != nil {
				return err
			}
			want, err := time.Parse(time.RFC3339, test.instant)
			if err != nil {
				return err
			}
			got := temporalOne(t, tx, expr.AsSelected(expr.ResolveLocal(expr.Param(orm.TimeExpr(f.Local), local), ny, expr.PostgresStandardTime)))
			if !got.UTC().Equal(want) {
				t.Fatal(test.local, got, want)
			}
		}
		var dates []temporal.Date
		var clocks []temporal.Time
		var locals []temporal.LocalDateTime
		var maybeDates []value.Nullable[temporal.Date]
		for _, s := range samples {
			dates = append(dates, s.Date)
			clocks = append(clocks, s.Clock)
			locals = append(locals, s.Local)
			maybeDates = append(maybeDates, s.MaybeDate)
		}
		temporalValues(t, tx, expr.AsSelected(expr.DateOf(expr.Value(orm.TimeExpr(f.Local)))), dates)
		temporalValues(t, tx, expr.AsSelected(expr.TimeOf(expr.Value(orm.TimeExpr(f.Local)))), clocks)
		temporalValues(t, tx, expr.AsSelected(expr.CombineDateTime(expr.Value(orm.TimeExpr(f.Date)), expr.Value(orm.TimeExpr(f.Clock)))), locals)
		temporalValues(t, tx, expr.AsSelected(expr.DateOfNullable(expr.Value(orm.NullableTimeExpr(f.MaybeLocal)))), maybeDates)
		return nil
	})
}

func TestPostgresTemporalCalendarAndElapsedArithmetic(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		f := temporalqueries.Samples
		ny, err := expr.LoadTimeZone("America/New_York")
		if err != nil {
			return err
		}
		elapsed, err := temporal.Elapsed(24 * time.Hour)
		if err != nil {
			return err
		}
		at := expr.Param(orm.TimeExpr(f.At), samples[0].At)
		day := expr.AddInstantInterval(at, temporal.Days(1), ny)
		if got := temporalOne(t, tx, expr.AsSelected(day)); !got.Equal(samples[1].At) || got.Sub(samples[0].At) != 23*time.Hour {
			t.Fatal("calendar day across DST", got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.AddInstantInterval(at, elapsed, ny))); !got.Equal(samples[0].At.Add(24 * time.Hour)) {
			t.Fatal("elapsed day across DST", got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.SubtractInstantInterval(day, temporal.Days(1), ny))); !got.Equal(samples[0].At) {
			t.Fatal("subtract calendar day", got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.SubtractInstantInterval(expr.AddInstantInterval(at, elapsed, ny), elapsed, ny))); !got.Equal(samples[0].At) {
			t.Fatal("subtract elapsed", got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.AddInstantInterval(expr.Param(orm.TimeExpr(f.Instant), samples[0].Instant), temporal.Days(1), ny))); got != samples[1].Instant {
			t.Fatal("Foundry instant", got)
		}
		jan, err := temporal.ParseDate("2024-01-31")
		if err != nil {
			return err
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.AddDateInterval(expr.Param(orm.TimeExpr(f.Date), jan), temporal.Months(1)))).String(); got != "2024-02-29T00:00:00" {
			t.Fatal("end-of-month clipping", got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.SubtractDateInterval(expr.Param(orm.TimeExpr(f.Date), jan), temporal.Days(1)))).String(); got != "2024-01-30T00:00:00" {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.AddDateDays(expr.Param(orm.TimeExpr(f.Date), jan), 29))).String(); got != "2024-02-29" {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.DateDifference(expr.AddDateDays(expr.Param(orm.TimeExpr(f.Date), jan), -29), expr.Param(orm.TimeExpr(f.Date), jan)))); got != -29 {
			t.Fatal(got)
		}
		local := expr.Param(orm.TimeExpr(f.Local), samples[0].Local)
		if got := temporalOne(t, tx, expr.AsSelected(expr.AddLocalInterval(local, temporal.Days(1)))); got != samples[1].Local {
			t.Fatal("local calendar arithmetic", got)
		}
		if got := temporalOne(t, tx, expr.AsSelected(expr.SubtractLocalInterval(expr.AddLocalInterval(local, elapsed), elapsed))); got != samples[0].Local {
			t.Fatal(got)
		}
		midnight, err := temporal.ParseTime("00:00:00")
		if err != nil {
			return err
		}
		for _, test := range []struct {
			expression expr.Selected[temporalqueries.Sample, temporal.Time]
			want       string
		}{
			{expr.AsSelected(expr.AddClockElapsed(expr.Param(orm.TimeExpr(f.Clock), midnight), -time.Microsecond)), "23:59:59.999999"},
			{expr.AsSelected(expr.SubtractClockElapsed(expr.Param(orm.TimeExpr(f.Clock), midnight), time.Microsecond)), "23:59:59.999999"},
			{expr.AsSelected(expr.AddClockElapsed(expr.Param(orm.TimeExpr(f.Clock), midnight), 48*time.Hour)), "00:00:00"},
		} {
			if got := temporalOne(t, tx, test.expression).String(); got != test.want {
				t.Fatal(got, test.want)
			}
		}
		return nil
	})
}
