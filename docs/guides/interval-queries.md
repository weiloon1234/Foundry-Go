# Typed interval models and calculations

`temporal.Interval` is a generated model and projection field type. It retains signed calendar months, calendar days and elapsed time separately. The [consumer fixture](../../tests/fixtures/consumer/intervalqueries/) exercises queries, writes, relations, summaries and cursor pages through public APIs.

## Models and values

Declare an ordinary Go field such as `Period temporal.Interval`, or `MaybePeriod value.Nullable[temporal.Interval]`. Generation supplies typed draft setters, interval field descriptors and complete decoding through `codec.Interval()`. An explicit zero interval differs from an omitted draft field and SQL NULL.

```go
draft := intervalqueries.SampleDraft{}.
    SetPeriod(temporal.Months(1)).
    ClearMaybePeriod()

f := intervalqueries.SampleFields()
samples, err := intervalqueries.QueryIntervalSamples().
    Where(f.Period.Gte(temporal.Days(7))).
    OrderBy(f.Period.Asc()).
    All(ctx, db)
```

This is a partial draft; required fields must be supplied before creation. Returned models and their collections remain ordinary Go structs and slices.

`Months(int32)` and `Days(int32)` construct calendar components. `Elapsed(time.Duration)` and `NewInterval(months, days, elapsed)` return an error for sub-microsecond precision. The zero value is valid. Components are immutable and accessible through `Months()`, `Days()` and `Elapsed()`.

`ParseInterval` accepts Foundry's canonical signed text and PostgreSQL's `postgres`, `postgres_verbose`, `sql_standard` and `iso_8601` output formats. Parsing is bounded to 256 bytes and retains components without silently moving elapsed hours into calendar days. Text/JSON encoding uses canonical strings. Invalid input leaves unmarshal and codec scan destinations unchanged.

Month/day components must fit `int32`; the elapsed component must fit Go's `time.Duration` at whole-microsecond precision. PostgreSQL can store a wider elapsed component and infinity; those values fail decoding. A collected query discards all results if any later row fails. See [codec boundaries](database-codecs.md) and [PostgreSQL interval formats](https://www.postgresql.org/docs/18/datatype-datetime.html#DATATYPE-INTERVAL-OUTPUT).

## Comparison and relationships

Go `==` compares the three stored components. PostgreSQL interval equality and ordering assume 30 days per month and 24 hours per day. Consequently, `Months(1)`, `Days(30)` and 720 elapsed hours compare equal in SQL while remaining different Go values. [PostgreSQL comparison implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/timestamp.c)

Generated interval predicates, joins, primary-key lookup and conflict handling follow SQL comparison. Eager relation matching and aggregate attachment use the same comparison internally, while returned parent, target and pivot models keep their original components. Cursor pages preserve these SQL ties and append the model primary key as usual. Interval keys are supported, but applications should choose them only when this equality matches their domain identity.

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

```go
ny, err := query.LoadTimeZone("America/New_York")
if err != nil {
    return err
}
shifted := query.ShiftInstant(f.Start, f.Period, ny)
values, err := query.SelectValue(
    intervalqueries.QueryIntervalSamples(), shifted.Value(),
).All(ctx, db)
```

Dynamic shifts follow the [calendar arithmetic rules](temporal-calculations.md): months clip to the final day of a short month, calendar days honor the explicit zone, and elapsed hours retain elapsed meaning. Use `NegateInterval` to subtract a dynamic interval; literal `Add*Interval`/`Subtract*Interval` conveniences remain available.

PostgreSQL normalizes timestamp differences into 24-hour day components plus remaining elapsed time. A 25-hour instant difference therefore returns one day and one hour. Applying that result with `ShiftInstant` across a daylight-saving transition need not invert the original difference: its day component is calendar-based during application. Large local differences can fit the day component even when their total duration would exceed Go's `time.Duration`.

## Summaries, NULL and query phases

Interval fields expose `Sum`, `Avg`, `Min`, `Max`, counts and their usual filters/window adapters. `Sum` and `Avg` return `value.Nullable[temporal.Interval]`, including NULL for empty or all-NULL input. PostgreSQL owns interval averaging and whole-microsecond rounding; fractional months can become days and fractional days can become elapsed time. Overflow remains an execution or decode error.

```go
summary, err := intervalqueries.ProjectSummary(
    intervalqueries.QueryIntervalSamples(),
).
    SelectTotal(f.Period.Sum().Value()).
    SelectAverage(f.Period.Avg().Value()).
    SelectCount(query.Count[intervalqueries.Sample]().Value()).
    Query().RequireFirst(ctx, db)
```

Every calculation has `Nullable`, `Value` and `NullableValue` forms. Nullable binary operations require both operands to be nullable; use `NullableRow` or `Nullable` to widen the other operand explicitly. SQL NULL propagates. Selected forms retain aggregates and windows, for example `IntervalMonthsNullableValue(f.Period.Sum().Value())`. They cannot silently become row predicates.

Expression depth, node and parameter limits still apply. `query.MaxScalarSQLBytes` bounds cumulative scalar SQL expansion during one compiler run, excluding parameter contents; it is not a universal statement-byte limit. Nested calculations exceeding that work budget fail before execution. Arithmetic overflow, physical database types and stored values still require runtime checks.
