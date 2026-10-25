package temporalqueries_test

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
			if got := temporalOne(t, tx, expr.TruncateDate(expr.Param(f.Date, samples[0].Date), test.unit).Value()).String(); got != test.want {
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
			if got := temporalOne(t, tx, expr.TruncateLocal(expr.Param(f.Local, samples[0].Local), test.unit).Value()); got != want {
				t.Fatal(test.unit, got, want)
			}
		}
		at := expr.Param(f.At, samples[0].At)
		for _, test := range []struct {
			zone expr.TimeZone
			hour int
		}{{ny, 5}, {expr.UTCZone(), 0}} {
			got := temporalOne(t, tx, expr.TruncateInstant(at, expr.TimestampDay, test.zone).Value())
			if !got.Equal(time.Date(2024, 3, 9, test.hour, 0, 0, 0, time.UTC)) {
				t.Fatal(test.zone, got)
			}
		}
		if got := temporalOne(t, tx, expr.LocalAt(at, ny).Value()); got != samples[0].Local {
			t.Fatal(got, samples[0].Local)
		}
		if got := temporalOne(t, tx, expr.LocalAt(expr.Param(f.Instant, samples[0].Instant), ny).Value()); got != samples[0].Local {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, expr.TruncateInstant(expr.Param(f.Instant, samples[0].Instant), expr.TimestampDay, ny).Value()); !got.UTC().Equal(time.Date(2024, 3, 9, 5, 0, 0, 0, time.UTC)) {
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
			got := temporalOne(t, tx, expr.ResolveLocal(expr.Param(f.Local, local), ny, expr.PostgresStandardTime).Value())
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
		temporalValues(t, tx, expr.DateOf(expr.Value(f.Local)).Value(), dates)
		temporalValues(t, tx, expr.TimeOf(expr.Value(f.Local)).Value(), clocks)
		temporalValues(t, tx, expr.CombineDateTime(expr.Value(f.Date), expr.Value(f.Clock)).Value(), locals)
		temporalValues(t, tx, expr.DateOfNullable(expr.Value(f.MaybeLocal)).Value(), maybeDates)
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
		at := expr.Param(f.At, samples[0].At)
		day := expr.AddInstantInterval(at, temporal.Days(1), ny)
		if got := temporalOne(t, tx, day.Value()); !got.Equal(samples[1].At) || got.Sub(samples[0].At) != 23*time.Hour {
			t.Fatal("calendar day across DST", got)
		}
		if got := temporalOne(t, tx, expr.AddInstantInterval(at, elapsed, ny).Value()); !got.Equal(samples[0].At.Add(24 * time.Hour)) {
			t.Fatal("elapsed day across DST", got)
		}
		if got := temporalOne(t, tx, expr.SubtractInstantInterval(day, temporal.Days(1), ny).Value()); !got.Equal(samples[0].At) {
			t.Fatal("subtract calendar day", got)
		}
		if got := temporalOne(t, tx, expr.SubtractInstantInterval(expr.AddInstantInterval(at, elapsed, ny), elapsed, ny).Value()); !got.Equal(samples[0].At) {
			t.Fatal("subtract elapsed", got)
		}
		if got := temporalOne(t, tx, expr.AddInstantInterval(expr.Param(f.Instant, samples[0].Instant), temporal.Days(1), ny).Value()); got != samples[1].Instant {
			t.Fatal("Foundry instant", got)
		}
		jan, err := temporal.ParseDate("2024-01-31")
		if err != nil {
			return err
		}
		if got := temporalOne(t, tx, expr.AddDateInterval(expr.Param(f.Date, jan), temporal.Months(1)).Value()).String(); got != "2024-02-29T00:00:00" {
			t.Fatal("end-of-month clipping", got)
		}
		if got := temporalOne(t, tx, expr.SubtractDateInterval(expr.Param(f.Date, jan), temporal.Days(1)).Value()).String(); got != "2024-01-30T00:00:00" {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, expr.AddDateDays(expr.Param(f.Date, jan), 29).Value()).String(); got != "2024-02-29" {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, expr.DateDifference(expr.AddDateDays(expr.Param(f.Date, jan), -29), expr.Param(f.Date, jan)).Value()); got != -29 {
			t.Fatal(got)
		}
		local := expr.Param(f.Local, samples[0].Local)
		if got := temporalOne(t, tx, expr.AddLocalInterval(local, temporal.Days(1)).Value()); got != samples[1].Local {
			t.Fatal("local calendar arithmetic", got)
		}
		if got := temporalOne(t, tx, expr.SubtractLocalInterval(expr.AddLocalInterval(local, elapsed), elapsed).Value()); got != samples[0].Local {
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
			{expr.AddClockElapsed(expr.Param(f.Clock, midnight), -time.Microsecond).Value(), "23:59:59.999999"},
			{expr.SubtractClockElapsed(expr.Param(f.Clock, midnight), time.Microsecond).Value(), "23:59:59.999999"},
			{expr.AddClockElapsed(expr.Param(f.Clock, midnight), 48*time.Hour).Value(), "00:00:00"},
		} {
			if got := temporalOne(t, tx, test.expression).String(); got != test.want {
				t.Fatal(got, test.want)
			}
		}
		return nil
	})
}
