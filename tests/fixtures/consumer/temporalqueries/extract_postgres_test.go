package temporalqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
			{"year", expr.AsSelected(expr.Year(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64(s.Date.Year()) }},
			{"local year", expr.AsSelected(expr.Year(expr.Value(orm.TimeExpr(f.Local)))), func(s temporalqueries.Sample) int64 { return int64(s.Local.Date().Year()) }},
			{"month", expr.AsSelected(expr.Month(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64(s.Date.Month()) }},
			{"day", expr.AsSelected(expr.Day(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64(s.Date.Day()) }},
			{"weekday", expr.AsSelected(expr.DayOfWeek(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64(calendar(s).Weekday()) }},
			{"ISO weekday", expr.AsSelected(expr.ISODayOfWeek(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64((calendar(s).Weekday()+6)%7) + 1 }},
			{"year day", expr.AsSelected(expr.DayOfYear(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64(calendar(s).YearDay()) }},
			{"ISO week", expr.AsSelected(expr.ISOWeek(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { _, week := calendar(s).ISOWeek(); return int64(week) }},
			{"ISO year", expr.AsSelected(expr.ISOYear(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { year, _ := calendar(s).ISOWeek(); return int64(year) }},
			{"quarter", expr.AsSelected(expr.Quarter(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64((s.Date.Month()-1)/3) + 1 }},
			{"century", expr.AsSelected(expr.Century(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64((s.Date.Year()-1)/100) + 1 }},
			{"decade", expr.AsSelected(expr.Decade(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64(s.Date.Year() / 10) }},
			{"millennium", expr.AsSelected(expr.Millennium(expr.Value(orm.TimeExpr(f.Date)))), func(s temporalqueries.Sample) int64 { return int64((s.Date.Year()-1)/1000) + 1 }},
			{"hour", expr.AsSelected(expr.Hour(expr.Value(orm.TimeExpr(f.Clock)))), func(s temporalqueries.Sample) int64 { return int64(s.Clock.Hour()) }},
			{"local hour", expr.AsSelected(expr.Hour(expr.Value(orm.TimeExpr(f.Local)))), func(s temporalqueries.Sample) int64 { return int64(s.Local.Time().Hour()) }},
			{"minute", expr.AsSelected(expr.Minute(expr.Value(orm.TimeExpr(f.Clock)))), func(s temporalqueries.Sample) int64 { return int64(s.Clock.Minute()) }},
			{"microseconds", expr.AsSelected(expr.Microseconds(expr.Value(orm.TimeExpr(f.Clock)))), func(s temporalqueries.Sample) int64 {
				return int64(s.Clock.Second())*1000000 + int64(s.Clock.Nanosecond()/1000)
			}},
			{"Unix millis", expr.AsSelected(expr.UnixMilliseconds(expr.Value(orm.TimeExpr(f.At)))), func(s temporalqueries.Sample) int64 { return s.At.UnixMilli() }},
			{"Foundry instant millis", expr.AsSelected(expr.UnixMilliseconds(expr.Value(orm.TimeExpr(f.Instant)))), func(s temporalqueries.Sample) int64 { return s.Instant.UTC().UnixMilli() }},
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
		temporalValues(t, tx, expr.AsSelected(expr.Second(expr.Value(orm.TimeExpr(f.Clock)))), seconds)
		temporalValues(t, tx, expr.AsSelected(expr.Milliseconds(expr.Value(orm.TimeExpr(f.Clock)))), milliseconds)
		temporalValues(t, tx, expr.AsSelected(expr.UnixSeconds(expr.Value(orm.TimeExpr(f.At)))), epochs)
		temporalValues(t, tx, expr.AsSelected(expr.JulianDay(expr.Value(orm.TimeExpr(f.Date)))), julian)
		temporalValues(t, tx, expr.AsSelected(expr.YearNullable(expr.Value(orm.NullableTimeExpr(f.MaybeDate)))), nullableYears)
		q := temporalqueries.Samples.Query()
		year := expr.Year(expr.Value(orm.TimeExpr(f.Date)))
		if items, err := orm.All(t.Context(), tx, expr.Where(q, expr.Eq(year, 2024))); err != nil || len(items) != 2 {
			t.Fatal(items, err)
		}
		counts := rows.OrderBy(rows.GroupBy(rows.Select(rows.From(q), temporalqueries.CalendarSummaries, rows.MapSelected(temporalqueries.CalendarSummaries.Year, expr.AsSelected(year)), rows.MapSelected(temporalqueries.CalendarSummaries.Rows, expr.AsSelected(expr.Count[temporalqueries.Sample]())), rows.MapSelected(temporalqueries.CalendarSummaries.Latest, expr.AsSelected(expr.Max(orm.TimeExpr(f.Date))))), expr.Group(year)), expr.Asc(year))
		if items, err := rows.Read(counts).All(t.Context(), tx); err != nil || len(items) != 3 || items[2].Rows != 2 {
			t.Fatal(items, err)
		}
		latest := expr.YearNullableValue(expr.AsSelected(expr.Max(orm.TimeExpr(f.Date))))
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
		temporalValues(t, tx, expr.AsSelected(expr.FromUnixMillis(expr.Value(orm.NumberExpr(f.Millis)))), expected)
		for _, millis := range []int64{-62135596800000, -86400001, -86400000, -1, 0, 1, 86400001, 253402300799999} {
			converted := expr.FromUnixMillis(expr.Param(orm.NumberExpr(f.Millis), millis))
			if got := temporalOne(t, tx, expr.AsSelected(converted)).UTC(); !got.Equal(time.UnixMilli(millis)) {
				t.Fatal("exact millisecond conversion", millis, got)
			}
			if got := temporalOne(t, tx, expr.AsSelected(expr.UnixMilliseconds(converted))); got != millis {
				t.Fatal("millisecond round trip", got, millis)
			}
		}
		before, _ := time.Parse(time.RFC3339Nano, "1969-12-31T23:59:59.999500Z")
		if got := temporalOne(t, tx, expr.AsSelected(expr.UnixMilliseconds(expr.Param(orm.TimeExpr(f.At), before)))); got != -1 {
			t.Fatal("negative submillisecond floor", got)
		}
		return nil
	})
}
