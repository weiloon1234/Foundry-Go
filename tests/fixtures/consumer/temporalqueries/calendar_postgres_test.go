package temporalqueries_test

import (
	"testing"
	"time"

	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresTemporalTruncationAndZones(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		f := temporalqueries.SampleFields()
		ny, err := query.LoadTimeZone("America/New_York")
		if err != nil {
			return err
		}
		for _, test := range []struct {
			unit query.DateUnit
			want string
		}{
			{query.DateDay, "2024-03-09"}, {query.DateWeek, "2024-03-04"}, {query.DateMonth, "2024-03-01"},
			{query.DateQuarter, "2024-01-01"}, {query.DateYear, "2024-01-01"}, {query.DateDecade, "2020-01-01"},
			{query.DateCentury, "2001-01-01"}, {query.DateMillennium, "2001-01-01"},
		} {
			if got := temporalOne(t, tx, query.TruncateDate(f.Date.Param(samples[0].Date), test.unit).Value()).String(); got != test.want {
				t.Fatal(test.unit, got, test.want)
			}
		}
		for _, test := range []struct {
			unit query.TimestampUnit
			want string
		}{
			{query.TimestampMicrosecond, "2024-03-09T12:34:56.123456"},
			{query.TimestampMillisecond, "2024-03-09T12:34:56.123"},
			{query.TimestampSecond, "2024-03-09T12:34:56"},
			{query.TimestampMinute, "2024-03-09T12:34:00"},
			{query.TimestampHour, "2024-03-09T12:00:00"},
			{query.TimestampDay, "2024-03-09T00:00:00"},
			{query.TimestampWeek, "2024-03-04T00:00:00"},
		} {
			want, err := temporal.ParseLocalDateTime(test.want)
			if err != nil {
				return err
			}
			if got := temporalOne(t, tx, query.TruncateLocal(f.Local.Param(samples[0].Local), test.unit).Value()); got != want {
				t.Fatal(test.unit, got, want)
			}
		}
		at := f.At.Param(samples[0].At)
		for _, test := range []struct {
			zone query.TimeZone
			hour int
		}{{ny, 5}, {query.UTCZone(), 0}} {
			got := temporalOne(t, tx, query.TruncateInstant(at, query.TimestampDay, test.zone).Value())
			if !got.Equal(time.Date(2024, 3, 9, test.hour, 0, 0, 0, time.UTC)) {
				t.Fatal(test.zone, got)
			}
		}
		if got := temporalOne(t, tx, query.LocalAt(at, ny).Value()); got != samples[0].Local {
			t.Fatal(got, samples[0].Local)
		}
		if got := temporalOne(t, tx, query.LocalAt(f.Instant.Param(samples[0].Instant), ny).Value()); got != samples[0].Local {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, query.TruncateInstant(f.Instant.Param(samples[0].Instant), query.TimestampDay, ny).Value()); !got.UTC().Equal(time.Date(2024, 3, 9, 5, 0, 0, 0, time.UTC)) {
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
			got := temporalOne(t, tx, query.ResolveLocal(f.Local.Param(local), ny, query.PostgresStandardTime).Value())
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
		temporalValues(t, tx, query.DateOf(f.Local).Value(), dates)
		temporalValues(t, tx, query.TimeOf(f.Local).Value(), clocks)
		temporalValues(t, tx, query.CombineDateTime(f.Date, f.Clock).Value(), locals)
		temporalValues(t, tx, query.DateOfNullable(f.MaybeLocal).Value(), maybeDates)
		return nil
	})
}

func TestPostgresTemporalCalendarAndElapsedArithmetic(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		f := temporalqueries.SampleFields()
		ny, err := query.LoadTimeZone("America/New_York")
		if err != nil {
			return err
		}
		elapsed, err := temporal.Elapsed(24 * time.Hour)
		if err != nil {
			return err
		}
		at := f.At.Param(samples[0].At)
		day := query.AddInstantInterval(at, temporal.Days(1), ny)
		if got := temporalOne(t, tx, day.Value()); !got.Equal(samples[1].At) || got.Sub(samples[0].At) != 23*time.Hour {
			t.Fatal("calendar day across DST", got)
		}
		if got := temporalOne(t, tx, query.AddInstantInterval(at, elapsed, ny).Value()); !got.Equal(samples[0].At.Add(24 * time.Hour)) {
			t.Fatal("elapsed day across DST", got)
		}
		if got := temporalOne(t, tx, query.SubtractInstantInterval(day, temporal.Days(1), ny).Value()); !got.Equal(samples[0].At) {
			t.Fatal("subtract calendar day", got)
		}
		if got := temporalOne(t, tx, query.SubtractInstantInterval(query.AddInstantInterval(at, elapsed, ny), elapsed, ny).Value()); !got.Equal(samples[0].At) {
			t.Fatal("subtract elapsed", got)
		}
		if got := temporalOne(t, tx, query.AddInstantInterval(f.Instant.Param(samples[0].Instant), temporal.Days(1), ny).Value()); got != samples[1].Instant {
			t.Fatal("Foundry instant", got)
		}
		jan, err := temporal.ParseDate("2024-01-31")
		if err != nil {
			return err
		}
		if got := temporalOne(t, tx, query.AddDateInterval(f.Date.Param(jan), temporal.Months(1)).Value()).String(); got != "2024-02-29T00:00:00" {
			t.Fatal("end-of-month clipping", got)
		}
		if got := temporalOne(t, tx, query.SubtractDateInterval(f.Date.Param(jan), temporal.Days(1)).Value()).String(); got != "2024-01-30T00:00:00" {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, query.AddDateDays(f.Date.Param(jan), 29).Value()).String(); got != "2024-02-29" {
			t.Fatal(got)
		}
		if got := temporalOne(t, tx, query.DateDifference(query.AddDateDays(f.Date.Param(jan), -29), f.Date.Param(jan)).Value()); got != -29 {
			t.Fatal(got)
		}
		local := f.Local.Param(samples[0].Local)
		if got := temporalOne(t, tx, query.AddLocalInterval(local, temporal.Days(1)).Value()); got != samples[1].Local {
			t.Fatal("local calendar arithmetic", got)
		}
		if got := temporalOne(t, tx, query.SubtractLocalInterval(query.AddLocalInterval(local, elapsed), elapsed).Value()); got != samples[0].Local {
			t.Fatal(got)
		}
		midnight, err := temporal.ParseTime("00:00:00")
		if err != nil {
			return err
		}
		for _, test := range []struct {
			expression query.Expression[temporalqueries.Sample, temporal.Time]
			want       string
		}{
			{query.AddClockElapsed(f.Clock.Param(midnight), -time.Microsecond).Value(), "23:59:59.999999"},
			{query.SubtractClockElapsed(f.Clock.Param(midnight), time.Microsecond).Value(), "23:59:59.999999"},
			{query.AddClockElapsed(f.Clock.Param(midnight), 48*time.Hour).Value(), "00:00:00"},
		} {
			if got := temporalOne(t, tx, test.expression).String(); got != test.want {
				t.Fatal(got, test.want)
			}
		}
		return nil
	})
}
