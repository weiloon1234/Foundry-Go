package temporalqueries_test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresTypedTemporalExtraction(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		f := temporalqueries.Samples
		calendar := func(s temporalqueries.Sample) time.Time {
			return time.Date(s.Date.Year(), s.Date.Month(), s.Date.Day(), 0, 0, 0, 0, time.UTC)
		}
		for _, test := range []struct {
			name       string
			expression expr.Selected[temporalqueries.Sample, int64]
			expected   func(temporalqueries.Sample) int64
		}{
			{"year", expr.Year(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Date.Year()) }},
			{"local year", expr.Year(expr.Value(f.Local)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Local.Date().Year()) }},
			{"month", expr.Month(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Date.Month()) }},
			{"day", expr.Day(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Date.Day()) }},
			{"weekday", expr.DayOfWeek(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64(calendar(s).Weekday()) }},
			{"ISO weekday", expr.ISODayOfWeek(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64((calendar(s).Weekday()+6)%7) + 1 }},
			{"year day", expr.DayOfYear(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64(calendar(s).YearDay()) }},
			{"ISO week", expr.ISOWeek(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { _, week := calendar(s).ISOWeek(); return int64(week) }},
			{"ISO year", expr.ISOYear(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { year, _ := calendar(s).ISOWeek(); return int64(year) }},
			{"quarter", expr.Quarter(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64((s.Date.Month()-1)/3) + 1 }},
			{"century", expr.Century(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64((s.Date.Year()-1)/100) + 1 }},
			{"decade", expr.Decade(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Date.Year() / 10) }},
			{"millennium", expr.Millennium(expr.Value(f.Date)).Value(), func(s temporalqueries.Sample) int64 { return int64((s.Date.Year()-1)/1000) + 1 }},
			{"hour", expr.Hour(expr.Value(f.Clock)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Clock.Hour()) }},
			{"local hour", expr.Hour(expr.Value(f.Local)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Local.Time().Hour()) }},
			{"minute", expr.Minute(expr.Value(f.Clock)).Value(), func(s temporalqueries.Sample) int64 { return int64(s.Clock.Minute()) }},
			{"microseconds", expr.Microseconds(expr.Value(f.Clock)).Value(), func(s temporalqueries.Sample) int64 {
				return int64(s.Clock.Second())*1000000 + int64(s.Clock.Nanosecond()/1000)
			}},
			{"Unix millis", expr.UnixMilliseconds(expr.Value(f.At)).Value(), func(s temporalqueries.Sample) int64 { return s.At.UnixMilli() }},
			{"Foundry instant millis", expr.UnixMilliseconds(expr.Value(f.Instant)).Value(), func(s temporalqueries.Sample) int64 { return s.Instant.UTC().UnixMilli() }},
		} {
			t.Run(test.name, func(t *testing.T) {
				want := make([]int64, len(samples))
				for i, s := range samples {
					want[i] = test.expected(s)
				}
				temporalValues(t, tx, test.expression, want)
			})
		}
		var seconds, milliseconds, epochs, julian []decimal.Decimal
		var nullableYears []value.Nullable[int64]
		for _, s := range samples {
			second, err := decimal.Parse(fmt.Sprintf("%d.%06d", s.Clock.Second(), s.Clock.Nanosecond()/1000))
			if err != nil {
				return err
			}
			seconds = append(seconds, second)
			millisecond, err := decimal.Parse(new(big.Rat).SetFrac(big.NewInt(int64(s.Clock.Second())*1000000+int64(s.Clock.Nanosecond()/1000)), big.NewInt(1000)).FloatString(3))
			if err != nil {
				return err
			}
			milliseconds = append(milliseconds, millisecond)
			epoch, err := decimal.Parse(new(big.Rat).SetFrac(big.NewInt(s.At.Unix()*1000000+int64(s.At.Nanosecond()/1000)), big.NewInt(1000000)).FloatString(6))
			if err != nil {
				return err
			}
			epochs = append(epochs, epoch)
			julian = append(julian, decimal.FromInt64(calendar(s).Unix()/86400+2440588))
			if date, present := s.MaybeDate.Get(); present {
				nullableYears = append(nullableYears, value.Of(int64(date.Year())))
			} else {
				nullableYears = append(nullableYears, value.Null[int64]())
			}
		}
		temporalValues(t, tx, expr.Second(expr.Value(f.Clock)).Value(), seconds)
		temporalValues(t, tx, expr.Milliseconds(expr.Value(f.Clock)).Value(), milliseconds)
		temporalValues(t, tx, expr.UnixSeconds(expr.Value(f.At)).Value(), epochs)
		temporalValues(t, tx, expr.JulianDay(expr.Value(f.Date)).Value(), julian)
		temporalValues(t, tx, expr.YearNullable(expr.Value(f.MaybeDate)).Value(), nullableYears)
		q := temporalqueries.Samples.Query()
		year := expr.Year(expr.Value(f.Date))
		if items, err := expr.Where(q, year.Eq(2024)).All(t.Context(), tx); err != nil || len(items) != 2 {
			t.Fatal(items, err)
		}
		counts := rows.OrderBy(rows.GroupBy(rows.Select(rows.From(q), temporalqueries.CalendarSummaries, rows.MapSelected(temporalqueries.CalendarSummaries.Year, year.Value()), rows.MapSelected(temporalqueries.CalendarSummaries.Rows, expr.Count[temporalqueries.Sample]().Value()), rows.MapSelected(temporalqueries.CalendarSummaries.Latest, expr.Max(f.Date).Value())), expr.Group(year)), year.Asc())
		if items, err := rows.Read(counts).All(t.Context(), tx); err != nil || len(items) != 3 || items[2].Rows != 2 {
			t.Fatal(items, err)
		}
		latest := expr.YearNullableValue(expr.Max(f.Date).Value())
		if v := temporalOne(t, tx, latest); v != value.Of(int64(2024)) {
			t.Fatal(v)
		}
		return nil
	})
}

func TestPostgresExactUnixMillisConversion(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		f := temporalqueries.Samples
		var expected []temporal.DateTime
		for _, s := range samples {
			v, err := temporal.NewDateTime(time.UnixMilli(s.Millis))
			if err != nil {
				return err
			}
			expected = append(expected, v)
		}
		temporalValues(t, tx, expr.FromUnixMillis(expr.Value(f.Millis)).Value(), expected)
		for _, millis := range []int64{-62135596800000, -86400001, -86400000, -1, 0, 1, 86400001, 253402300799999} {
			converted := expr.FromUnixMillis(expr.Param(f.Millis, millis))
			if got := temporalOne(t, tx, converted.Value()).UTC(); !got.Equal(time.UnixMilli(millis)) {
				t.Fatal("exact millisecond conversion", millis, got)
			}
			if got := temporalOne(t, tx, expr.UnixMilliseconds(converted).Value()); got != millis {
				t.Fatal("millisecond round trip", got, millis)
			}
		}
		before, _ := time.Parse(time.RFC3339Nano, "1969-12-31T23:59:59.999500Z")
		if got := temporalOne(t, tx, expr.UnixMilliseconds(expr.Param(f.At, before)).Value()); got != -1 {
			t.Fatal("negative submillisecond floor", got)
		}
		return nil
	})
}
