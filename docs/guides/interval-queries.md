# Typed interval models and calculations

`temporal.Interval` is supported by handwritten ORM columns and rows fields. It retains signed calendar months, calendar days and elapsed time separately. The [consumer fixture](../../tests/fixtures/consumer/intervalqueries/) exercises queries, writes, relations, summaries and cursor pages through public APIs.

## Models and values

Declare an ordinary Go field such as `Period temporal.Interval`, or `MaybePeriod value.Nullable[temporal.Interval]`. The model directive infers interval handles backed by `codec.Interval()`; no role tag is needed. A supplied zero interval differs from an omitted assignment and SQL NULL.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database/orm/expr`, `database/orm/rows`, `temporal`, `value`.

This complete scalar fixture includes ordinary and nullable intervals:

<!-- compiled-source: tests/fixtures/goway/models/scalars/scalars.go -->
```go
package scalars

import (
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=scalar_records var=Records key=ID identity=false
type Record struct {
	ID            int64
	Name          string
	MaybeName     value.Nullable[string]
	Date          temporal.Date
	MaybeDate     value.Nullable[temporal.Date]
	Local         temporal.LocalDateTime
	MaybeLocal    value.Nullable[temporal.LocalDateTime]
	Instant       temporal.DateTime
	MaybeInstant  value.Nullable[temporal.DateTime]
	Clock         temporal.Time
	MaybeClock    value.Nullable[temporal.Time]
	Period        temporal.Interval
	MaybePeriod   value.Nullable[temporal.Interval]
	Millis        int64
	MaybeMillis   value.Nullable[int64]
	Latitude      float64
	MaybeLatitude value.Nullable[float64]
}
```

The example binds persisted fields to their typed columns. Returned models and results remain Go structs and slices.

`Months(int32)` and `Days(int32)` construct calendar components. `Elapsed(time.Duration)` and `NewInterval(months, days, elapsed)` return an error for sub-microsecond precision. The zero value is valid. Components are immutable and accessible through `Months()`, `Days()` and `Elapsed()`.

`ParseInterval` accepts Foundry's canonical signed text and PostgreSQL's `postgres`, `postgres_verbose`, `sql_standard` and `iso_8601` output formats. Parsing is bounded to 256 bytes and retains components without silently moving elapsed hours into calendar days. Text/JSON encoding uses canonical strings. Invalid input leaves unmarshal and codec scan destinations unchanged.

Month/day components must fit `int32`; the elapsed component must fit Go's `time.Duration` at whole-microsecond precision. PostgreSQL can store a wider elapsed component and infinity; those values fail decoding. A collected query discards all results if any later row fails. See [codec boundaries](database-codecs.md) and [PostgreSQL interval formats](https://www.postgresql.org/docs/18/datatype-datetime.html#DATATYPE-INTERVAL-OUTPUT).

## Comparison and relationships

Go `==` compares the three stored components. PostgreSQL interval equality and ordering assume 30 days per month and 24 hours per day. Consequently, `Months(1)`, `Days(30)` and 720 elapsed hours compare equal in SQL while remaining different Go values. [PostgreSQL comparison implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/timestamp.c)

ORM interval predicates, joins, primary-key lookup and conflict handling follow SQL comparison. Eager relation matching and aggregate attachment use the same comparison internally, while returned parent, target and pivot models keep their original components. Cursor pages preserve these SQL ties and append the model primary key as usual. Interval keys are supported, but applications should choose them only when this equality matches their domain identity.

## Calculations

All operands retain the owning model or query scope. These functions use the shared expression AST and parameterized compiler; no raw SQL is needed.

| Function | Result |
| --- | --- |
| `AddIntervals(left, right)` / `SubtractIntervals(left, right)` | Component-wise interval arithmetic |
| `NegateInterval(input)` | Interval with negated components |
| `IntervalMonths(input)` | `int32`, including the months represented as years |
| `IntervalDays(input)` | `int32`, calendar-day component |
| `IntervalElapsed(input)` | `time.Duration`, excluding calendar months/days |
| `LocalDifference(left, right)` | Local date-time difference as an interval |
| `InstantDifference(left, right)` | Instant difference as an interval; accepts `time.Time` and `temporal.DateTime` |
| `ClockDifference(left, right)` | Signed clock difference without midnight wrapping |
| `ShiftDate(date, interval)` | `temporal.LocalDateTime` |
| `ShiftLocal(local, interval)` | `temporal.LocalDateTime` |
| `ShiftInstant(instant, interval, zone)` | Same Go instant type |

The dynamic examples below use this application model, with `Samples` as its generated table:

<!-- compiled-source: tests/fixtures/consumer/intervalqueries/models.go#interval-queries -->
```go
//foundry:model table=interval_samples var=Samples key=ID identity=false
type Sample struct {
	ID          int `foundry:"member=ID,key-role=lookup"`
	Period      temporal.Interval
	MaybePeriod value.Nullable[temporal.Interval]
	Start       time.Time
	End         temporal.DateTime
	LocalStart  temporal.LocalDateTime
	LocalEnd    temporal.LocalDateTime
	ClockStart  temporal.Time
	ClockEnd    temporal.Time
	Date        temporal.Date
}
```

`ShiftedStart` shifts each sample's start instant by a caller-supplied interval in a named zone:

<!-- compiled-source: tests/fixtures/consumer/intervalqueries/orm.go#interval-queries-2 -->
```go
func ShiftedStart(period temporal.Interval, zoneName string) (expr.Selected[Sample, time.Time], error) {
	zone, err := expr.LoadTimeZone(zoneName)
	if err != nil {
		return expr.Selected[Sample, time.Time]{}, err
	}
	return expr.AsSelected(expr.ShiftInstant(expr.Value(Samples.Start), expr.Param(Samples.Period, period), zone)), nil
}
```

Dynamic shifts follow the [calendar arithmetic rules](temporal-calculations.md): months clip to the final day of a short month, calendar days honor the explicit zone, and elapsed hours retain elapsed meaning. Use `NegateInterval` to subtract a dynamic interval; literal `Add*Interval`/`Subtract*Interval` conveniences remain available.

PostgreSQL normalizes timestamp differences into 24-hour day components plus remaining elapsed time. A 25-hour instant difference therefore returns one day and one hour. Applying that result with `ShiftInstant` across a daylight-saving transition need not invert the original difference: its day component is calendar-based during application. Large local differences can fit the day component even when their total duration would exceed Go's `time.Duration`.

## Summaries, NULL and query phases

Use `expr.SumInterval`, `expr.AvgInterval`, `expr.Min`, `expr.Max`, counts and their filters/window adapters. `SumInterval` and `AvgInterval` return `value.Nullable[temporal.Interval]`, including NULL for empty or all-NULL input. PostgreSQL owns interval averaging and whole-microsecond rounding; fractional months can become days and fractional days can become elapsed time. Overflow remains an execution or decode error.

`Summary` is the result struct with nullable `Total` and `Average` intervals and an `int64` count field, and `Summaries` is its `rows.Define` schema. `PeriodSummary` sums, averages and counts the periods:

<!-- compiled-source: tests/fixtures/consumer/intervalqueries/orm.go#interval-queries-3 -->
```go
func PeriodSummary() rows.Query[Sample, Summary] {
	return rows.Select(rows.From(Samples.Query()), Summaries,
		rows.MapSelected(Summaries.Total, expr.AsSelected(expr.SumInterval(Samples.Period))),
		rows.MapSelected(Summaries.Average, expr.AsSelected(expr.AvgInterval(Samples.Period))),
		rows.MapSelected(Summaries.Count, expr.AsSelected(expr.Count[Sample]())))
}
```

Every calculation has `Nullable`, `Value` and `NullableValue` forms. Nullable binary operations require both operands to be nullable; use `NullableRow` or `Nullable` to widen the other operand explicitly. SQL NULL propagates. Selected forms retain aggregates and windows, for example `expr.IntervalMonthsNullableValue(expr.AsSelected(expr.SumInterval(f.Period)))`. They cannot silently become row predicates.

Expression depth, node and parameter limits still apply. The shared compiler bounds cumulative scalar SQL expansion during one compiler run, excluding parameter contents; it is not a universal statement-byte limit. Nested calculations exceeding that work budget fail before execution. Arithmetic overflow, physical database types and stored values still require runtime checks.
