# Typed RANGE frames and named windows

These APIs extend the [window-function contracts](model-windows.md), using the same query AST, result codecs, scope validation and execution runtime. The [independent consumer](../../tests/fixtures/consumer/framequeries/) contains numeric, temporal, named-window and composition examples.

## Numeric distances

```go
orders := models.QueryOrders()
o := models.OrderFields()
r := query.NumericRange(
    query.WindowFor(orders).PartitionBy(o.BuyerID.Group()),
    o.TotalCents.Value(),
)
window := r.Between(r.Preceding(500), r.CurrentRow())
totals, err := query.SelectValue(orders, o.TotalCents.Sum().Over(window)).All(ctx, db)
```

`NumericRange` retains the ordering expression's exact Go type in its distance API. An integer, exact decimal and floating-point value have different `Preceding`/`Following` arguments. `NullableNumericRange` accepts a nullable ordering expression while retaining non-nullable distances. Both support computed selected numeric values, including ordinary aggregates over grouped rows. Nested windows remain invalid at the same SELECT level.

The supplied window must not already have ordering. `Between` establishes the single ordering required for value-distance RANGE; another appended ordering is rejected. Use `r.Desc()` to reverse the window ordering. Preceding/following follow that direction. Window ordering does not order the final result query.

Obtain positions from the range builder: `r.CurrentRow()`, `r.UnboundedPreceding()` and `r.UnboundedFollowing()`. `RangeBoundary[Scope, Distance]` keeps both types, so ordinary row offsets and differently typed distances cannot enter the frame. `Between` returns the normal `Window[Scope]`, supporting exclusions and naming. It replaces an earlier frame; apply exclusions after completing it.

Distances are encoded once, bound as parameters and explicitly cast using the codec's representation. Negative, non-finite, NULL and invalid codec values fail before execution, even if the query would return no rows. Decimal distances stay exact. Zero distances use peer semantics; legal empty boundary intervals remain valid. See PostgreSQL's [frame rules](https://www.postgresql.org/docs/18/sql-expressions.html#SYNTAX-WINDOW-FUNCTIONS).

## Calendar and elapsed distances

```go
r := query.TemporalRange(query.WindowFor(samples), fields.At.Value())
calendarDay := r.Between(r.Preceding(temporal.Days(1)), r.CurrentRow())

elapsed, err := temporal.Elapsed(24 * time.Hour)
if err != nil {
    return err
}
elapsedDay := r.Between(r.Preceding(elapsed), r.CurrentRow())
```

`TemporalRange` supports `time.Time`, `temporal.DateTime`, `temporal.LocalDateTime`, `temporal.Date` and `temporal.Time`. `NullableTemporalRange` accepts their nullable expressions. Distances are immutable `temporal.Interval` values, with calendar months, calendar days and elapsed time kept separate. Use `Months`, `Days`, `Elapsed`, or `NewInterval(months, days, elapsed)` for combined signed components. Elapsed time must contain whole microseconds; finer precision is rejected. Its range is Go's `time.Duration`; calendar components use signed 32-bit counts.

A calendar day can differ from 24 elapsed hours across a timezone transition. PostgreSQL applies timestamp-with-time-zone calendar intervals using the connection's timezone. Configure that timezone deliberately when calendar boundaries matter. Local timestamps and dates retain their own wall/calendar meaning. Month-end arithmetic follows PostgreSQL, including clamping to a valid day. The consumer covers New York's DST transition and leap-year month ends.

Intervals are bound with explicit signs for each component, preserving their meaning under different PostgreSQL interval styles. RANGE sign validation follows PostgreSQL's [interval comparison implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/timestamp.c): a 30-day-month comparison checks whether the interval is negative, while actual evaluation keeps calendar components. It is not a conversion of calendar arithmetic into elapsed hours.

For time-of-day ordering, calendar months/days are rejected because PostgreSQL otherwise ignores them; use an elapsed interval. RANGE does not wrap around midnight. [PostgreSQL time-range implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/date.c)

`Interval` supplies these finite calendar/elapsed distances. It does not introduce a generated model interval codec or promise coverage of PostgreSQL's infinite intervals. Database arithmetic overflow and physical-schema incompatibility remain runtime errors.

## Reusable named definitions

```go
base := query.WindowFor(orders).
    PartitionBy(o.BuyerID.Group()).Named("by_buyer")
ordered := base.OrderBy(o.TotalCents.Asc(), o.ID.Asc()).Named("ordered")
recent := ordered.RowsBetween(query.Preceding(2), query.CurrentRow()).Named("recent")

count := query.Count[models.Order]().Over(recent)
total := o.TotalCents.Sum().Over(recent)
```

Each descriptor carries its definition. Reusing `recent` emits one WINDOW definition, with dependencies before dependents, and uses `OVER "recent"`. The scope remains typed. Define a name once and share the returned descriptor; different declarations with the same name in one SELECT are rejected. Names are validated and quoted at the declaration boundary. There are no string-based lookups when using a descriptor.

A direct named reference may include a frame. Extending or inheriting a named window has stricter rules: its definition must be frameless, no PARTITION BY may be added, and ORDER BY may be added only when the base has no ordering. The extension owns its frame. Even a second name copying a framed definition is rejected. These distinctions follow PostgreSQL's [WINDOW inheritance rules](https://www.postgresql.org/docs/18/sql-select.html#SQL-WINDOW).

Definitions are local to a SELECT. Nested scalar/derived queries may use the same name for independent definitions. CTE references inside definitions remain discoverable, and explicit correlated fields retain their parent scope. A named partition/order containing an ordinary aggregate establishes grouping just as its inline equivalent does. Distinct selection, result counting and generated projections retain their existing contracts.

Dependency depth/node limits, cycle detection, duplicate-name checks and nested-window checks run before execution. Compiler bounds do not limit database sorting/partition memory; use appropriate indexes, cancellation deadlines and input scopes.
