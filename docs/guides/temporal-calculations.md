# Typed temporal queries

Temporal calculations share the query AST, parameters, scope checking and result codecs used by [scalar calculations](scalar-calculations.md). The [consumer fixture](../../tests/fixtures/consumer/temporalqueries/) declares ordinary model fields for `time.Time`, `temporal.DateTime`, `temporal.Date`, `temporal.LocalDateTime`, `temporal.Time` and their nullable forms.

PostgreSQL 18 is the tested backend for these operations, including its explicit-timezone `date_add` and `date_subtract` functions.

## Calendar fields and instants

Calendar extraction accepts dates or local date-times. Clock extraction accepts times or local date-times. For an instant, select the timezone explicitly before extracting calendar or clock components:

```go
f := temporalqueries.SampleFields()
ny, err := query.LoadTimeZone("America/New_York")
if err != nil {
    return err
}
year := query.Year(query.LocalAt(f.At, ny))
samples, err := temporalqueries.QueryTemporalSamples().
    Where(year.Eq(2024)).
    OrderBy(f.ID.Asc()).
    All(ctx, db)
```

The returned `samples` is `[]temporalqueries.Sample`. These expressions run in PostgreSQL; sorting a loaded slice with Go's `slices` package remains a separate in-memory operation.

Calendar functions include `Year`, `Month`, `Day`, `DayOfWeek`, `ISODayOfWeek`, `DayOfYear`, `ISOWeek`, `ISOYear`, `Quarter`, `Century`, `Decade`, `Millennium` and `JulianDay`. Clock functions include `Hour`, `Minute`, `Second`, `Milliseconds` and `Microseconds`. Weekdays use Sunday = 0 for `DayOfWeek` and Monday = 1 for `ISODayOfWeek`; ISO week-years can differ from calendar years.

Integral components return `int64`. `Second`, `Milliseconds`, `JulianDay` and `UnixSeconds` return exact `decimal.Decimal`. Milliseconds and microseconds include the **whole seconds component**, matching PostgreSQL `EXTRACT`: `12:34:56.123456` has `56123.456` milliseconds and `56123456` microseconds. `JulianDay` includes a fractional day for local date-time inputs. [PostgreSQL extraction](https://www.postgresql.org/docs/18/functions-datetime.html#FUNCTIONS-DATETIME-EXTRACT)

## Truncation and timezone resolution

| Operation | Input | Result |
| --- | --- | --- |
| `TruncateDate(field, query.DateMonth)` | `temporal.Date` | `temporal.Date` |
| `TruncateLocal(field, query.TimestampHour)` | `temporal.LocalDateTime` | `temporal.LocalDateTime` |
| `TruncateInstant(field, query.TimestampDay, zone)` | `time.Time` or `temporal.DateTime` | Same instant type |
| `LocalAt(field, zone)` | Either instant type | `temporal.LocalDateTime` |
| `ResolveLocal(field, zone, query.PostgresStandardTime)` | `temporal.LocalDateTime` | `temporal.DateTime` |
| `DateOf(field)` / `TimeOf(field)` | `temporal.LocalDateTime` | `temporal.Date` / `temporal.Time` |
| `CombineDateTime(date, clock)` | Date and time in the same scope | `temporal.LocalDateTime` |

Date and timestamp truncation units have distinct Go types. Invalid zero units or explicit invalid conversions fail before execution, even with `Limit(0)`. Instant operations require an explicit `TimeZone`; `UTCZone()` selects UTC. `LoadTimeZone` validates against Go's timezone database, while execution uses PostgreSQL's database. Their versions may differ, and server-side resolution can still fail.

`ResolveLocal` explicitly selects PostgreSQL's resolution of daylight-saving gaps and overlaps. For ordinary DST transitions it prefers the standard-time interpretation. In New York, `2024-03-10T02:30:00` resolves to `07:30Z`, while `2024-11-03T01:30:00` resolves to `06:30Z`. This SQL policy differs from `temporal.LocalDateTime.In`, which rejects nonexistent or ambiguous instants. Zero resolution policy is invalid. [PostgreSQL ambiguous timestamps](https://www.postgresql.org/docs/18/datetime-invalid-input.html)

## Arithmetic and Unix time

`AddDateDays(date, int32)` returns a date; negative days subtract. `DateDifference(left, right)` returns signed whole days as `int64`.

`AddDateInterval`/`SubtractDateInterval` return local date-times because intervals can include clock components. `AddLocalInterval`/`SubtractLocalInterval` preserve local date-times. `AddInstantInterval`/`SubtractInstantInterval` preserve the instant's Go type and require an explicit timezone:

```go
tomorrow := query.AddInstantInterval(f.At, temporal.Days(1), ny)
elapsed, err := temporal.Elapsed(24 * time.Hour)
if err != nil {
    return err
}
after24Hours := query.AddInstantInterval(f.At, elapsed, ny)
```

Calendar months clip to the destination month's final day. A calendar day across a DST transition can span 23 or 25 elapsed hours. Elapsed durations keep their elapsed meaning. `AddClockElapsed`/`SubtractClockElapsed` take `time.Duration`, wrap at midnight and reject sub-microsecond values. Calendar and elapsed components remain separate in `temporal.Interval`. [PostgreSQL date/time arithmetic](https://www.postgresql.org/docs/18/functions-datetime.html)

`FromUnixMillis(integer)` returns `temporal.DateTime`. The compiler converts the exact integer to PostgreSQL's integer-millisecond interval representation and adds it to the epoch in UTC. It avoids floating-point conversion and repeated expansion of the input expression. `UnixMilliseconds(instant)` returns `int64`, rounding fractional milliseconds down, including before 1970. `UnixSeconds(instant)` retains microseconds as an exact decimal.

[Typed interval queries](interval-queries.md) extend these literal operations with interval model fields, summaries, differences, component extraction and dynamic `ShiftDate`/`ShiftLocal`/`ShiftInstant` operands.

`TransactionTime(source)` returns PostgreSQL's transaction start instant as `temporal.DateTime`; repeated statements within one transaction see the same value. It uses the database clock. For controllable application time, pass a value from Foundry's clock through a typed parameter instead.

## NULL, phases and failures

Each temporal operation has a nullable counterpart, such as `YearNullable` and `AddInstantIntervalNullable`. SQL NULL propagates. Binary nullable operations require explicit widening of any non-null input using `NullableRow`. Use `Coalesce` for a declared fallback.

The `Value` forms accept selected expressions, including aggregates and windows. For example, `YearNullableValue(f.Date.Max().Value())` retains aggregate semantics, while `UnixMillisecondsValue(FromUnixMillisValue(query.RowNumber(window)))` retains window semantics. Row constructors cannot accept selected expressions. Existing grouping, HAVING and nested SELECT checks still apply.

Dates and timestamps retain Foundry's years 1–9999 and whole-microsecond codec contracts. PostgreSQL can represent values outside this range, but result decoding rejects them and collected reads publish no partial slice. Database arithmetic overflow remains a database error; callers can use a savepoint when recovering within a transaction. Invalid units, zero zones, unsupported resolution policies and sub-microsecond elapsed values fail before I/O. Cancellation follows the normal query runtime.

Interval operands in this increment are typed literal values. Interval-valued model codecs, interval result expressions and general timestamp differences remain separate advanced-query work; no raw SQL is needed for the operations documented here.
