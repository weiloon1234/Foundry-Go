# Typed window functions

Window expressions retain their input scope and concrete Go result type. They populate generated projection fields, select a single value, or order a read-only query. The [consumer declarations](../../tests/fixtures/consumer/windowqueries/reports.go) and [PostgreSQL examples](../../tests/fixtures/consumer/windowqueries/windows_postgres_test.go) exercise the public API.

## Define and consume a window

```go
orders := models.QueryOrders()
o := models.OrderFields()
window := query.WindowFor(orders).
    PartitionBy(o.BuyerID.Group()).
    OrderBy(o.TotalCents.Asc(), o.ID.Asc())

positions, err := query.SelectValue(orders, query.RowNumber(window)).All(ctx, db)
// positions is []int64
```

`WindowFor` infers the scope from a model, alias, join, set or explicit correlation. It performs no database work and does not inherit query ordering or pagination. The enclosing query owns row filtering. `Window[Scope]{}` is a valid empty window, corresponding to `OVER ()`. Both forms derive immutably.

Window ordering controls the calculation. Use the enclosing query's `OrderBy` to order returned rows. Include a unique tie-breaker for stable individual positions; a separate ranking window can omit it when equal business values should remain peers.

For complete records, map window expressions through generated projection selections/builders. Persisted models are never partially hydrated or given undeclared computed fields. Expose the projection through `As` or a CTE before filtering its generated output fields—for example, `RankingRowFieldsAt(alias.Scope()).Number.Eq(1)` selects the first ranked row per partition. This reuses the [derived-record boundary](model-subqueries.md).

## Functions and result types

| API | Go expression result |
| --- | --- |
| `RowNumber(window)`, `Rank(window)`, `DenseRank(window)` | `int64` |
| `PercentRank(window)`, `CumeDist(window)` | `float64` |
| `NTile(buckets, window)` | `int32` |
| `aggregate.Over(window)` | The aggregate's existing result type |
| `Lag(input, offset, window)`, `Lead(input, offset, window)` | `value.Nullable[V]` for input `V` |
| `LagOr(input, offset, fallback, window)`, `LeadOr(...)` | Input type `V` |
| `FirstValue(input, window)`, `LastValue(input, window)`, `NthValue(input, n, window)` | `value.Nullable[V]` for input `V` |

Peers share ranks; `Rank` leaves gaps and `DenseRank` does not. Lag/lead use partition positions independently of the frame; negative offsets reverse direction. First/last/nth use the frame and can return NULL when empty. See [PostgreSQL window functions](https://www.postgresql.org/docs/18/functions-window.html).

Use `LagNullable`, `LeadNullable`, `FirstNullableValue`, `LastNullableValue` and `NthNullableValue` for already nullable inputs; they retain one wrapper. Ordinary helpers reject nested nullable input before execution. `LagOr`/`LeadOr` accept exactly the input type, including nullable types. They substitute for an absent row, retaining NULL from an existing row. Fallbacks are encoded once through the input codec during construction; invalid enum/codec values fail before execution.

Counts/existence retain `int64`/`bool`. Numeric summaries reuse aggregate compilation and codecs: integer sums/averages return nullable exact decimals; float summaries retain their floating representation. An aggregate receiving `Over` does not collapse rows. Ordinary aggregates inside a window's input or ordering do group the enclosing SELECT and follow its grouping rules.

## Frames and exclusions

```go
running := window.RowsBetween(query.UnboundedPreceding(), query.CurrentRow())
moving := window.RowsBetween(query.Preceding(2), query.CurrentRow())
whole := window.RowsBetween(query.UnboundedPreceding(), query.UnboundedFollowing())

totals, err := query.SelectValue(orders, o.TotalCents.Sum().Over(running)).All(ctx, db)
// totals is []value.Nullable[decimal.Decimal]
```

`RowsBetween` counts physical rows. `GroupsBetween` counts ordering peer groups and requires window ordering. They accept `FramePosition` values (`UnboundedPreceding`, `CurrentRow`, `UnboundedFollowing`) and nonnegative `FrameOffset` values (`Preceding`, `Following`). Offsets are SQL parameters.

`RangeBetween` accepts current/unbounded positions. Its current boundary covers peers. A row/group offset is not a typed value distance and fails Go compilation when passed here. For numeric or calendar/elapsed distances, use the [typed RANGE builders](window-ranges-and-names.md), which preserve the ordering value's distance type.

The default PostgreSQL frame includes rows through the current row's last peer, or the whole partition without ordering. An explicit whole-partition frame is useful for last-value and full-partition aggregates. Invalid boundary categories fail before execution; legal empty frames remain valid. See [PostgreSQL frame rules](https://www.postgresql.org/docs/18/sql-expressions.html#SYNTAX-WINDOW-FUNCTIONS).

`ExcludeCurrentRow`, `ExcludeGroup`, `ExcludeTies` and `ExcludeNone` derive exclusion behavior. Without an explicit frame they make the default RANGE frame explicit. Replacing the frame resets its exclusion; choose the frame first. Empty frames retain zero counts, false existence and nullable summaries.

## Composition and current limits

Generated fields/expressions enforce owner, value and result-nullability compatibility. Windows are not row or HAVING predicates; filter a derived output instead. Same-level nested windows fail before execution, while a scalar subquery owns its own window. A window aggregate can consume an explicitly captured outer value without moving to that outer SELECT.

Windows share CTE discovery, recursive/set composition, grouped-column checks, correlation requalification, bounds, parameters and decoding. Distinct ordering reuses the selected window and its bindings through an output position. Execution retains the [projection runtime's contracts](model-projections.md). Compiler bounds do not cap database partition/sort work; provide indexes and cancellation deadlines appropriate to the query.

[Aggregate filters](model-aggregate-filters.md) are available before `Over`, preserving frame membership and output rows. [Computed query keys](computed-query-keys.md) add row partitions through `Group()` and selected aggregate/scalar partitions through `PartitionByValues`. [Typed RANGE distances and named SQL WINDOW declarations](window-ranges-and-names.md) add numeric/temporal frames and reusable definitions with validated inheritance. `CountDistinct().Over(...)` is rejected because PostgreSQL does not support distinct window aggregates. PostgreSQL has no IGNORE NULLS or FROM LAST option for these functions; the helpers do not imply them.
