# Typed window functions

<!-- compiled-examples -->

Window functions in `database/orm/expr` return selected expressions. Map them with
`rows.MapSelected`, or use `rows.SelectScalar`. They cannot serve as row or HAVING
predicates. Filter a declared derived result to select rows by their computed rank.

## Define and consume a window

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/windowqueries/windows_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/windowqueries/windows_postgres_test.go -->
```go
base := models.Orders.Query()
o := models.Orders
peers := expr.WindowFor(rows.ModelScope(base)).PartitionBy(expr.Group(expr.Value(o.BuyerID))).OrderBy(expr.Value(o.TotalCents).Asc())
ordered := peers.OrderBy(expr.Value(o.ID).Asc())
```

`expr.WindowFor(rows.ModelScope(query))` binds a model scope; aliases and joins
supply their own scope. It does not inherit the query's ordering or window.
`expr.Window[S]{}` is a valid empty window. Window order controls the calculation;
query order controls returned row order. Include a unique tie-breaker for stable
individual positions; omit it from a peer-ranking window when ties should share rank.
The [complete example](../../tests/fixtures/consumer/windowqueries/windows_postgres_test.go)
maps these expressions into a handwritten ranking schema.

## Functions and result types

| `expr` function | Result type |
| --- | --- |
| `RowNumber`, `Rank`, `DenseRank` | `int64` |
| `PercentRank`, `CumeDist` | `float64` |
| `NTile` | `int32` |
| `Over(aggregate, window)` | The aggregate's existing result type |
| `Lag`, `Lead`, `FirstValue`, `LastValue`, `NthValue` | `value.Nullable[V]` for required input `V` |
| `LagOr`, `LeadOr` | Exact input type; fallback applies only to an absent row |

Lag/lead inspect partition positions independently of the frame; negative offsets
reverse direction. First/last/nth inspect the frame and can return NULL. Use
`LagNullable`, `LeadNullable`, `FirstNullableValue`, `LastNullableValue` and
`NthNullableValue` for nullable input without nesting wrappers. Fallback codecs
validate during construction. Ranking does not gain aggregate filters.

## Frames and exclusions

`RowsBetween` counts physical rows; `GroupsBetween` counts peers and requires
ordering. Both accept `UnboundedPreceding`, `CurrentRow`, `UnboundedFollowing` and
nonnegative `Preceding`/`Following` offsets. Offsets are parameters. `RangeBetween`
accepts current/unbounded positions; use [typed RANGE](window-ranges-and-names.md)
for numeric or temporal distances.

The default frame extends through the current row's last peer, or the whole
partition without ordering. Specify a whole-partition frame for a last-value or
total that should not stop at the current peer. Invalid boundary categories fail
before I/O; valid empty frames yield zero counts, false existence and NULL summaries.

Apply `ExcludeCurrentRow`, `ExcludeGroup`, `ExcludeTies` or `ExcludeNone` after
choosing the frame. An exclusion without an explicit frame makes the default RANGE
frame explicit. Replacing the frame resets exclusion.

## Composition and current limits

Apply aggregate `Filter` before `expr.Over`. Filtering changes contributions, not
output row count. COUNT DISTINCT windows and same-level nested windows are rejected.
An ordinary aggregate in a window argument/order/partition establishes grouping;
all ordinary references must then be grouped. A derived SELECT creates a new phase.

Named windows, CTE discovery, correlations and bindings share the compiler.
Compiler bounds do not cap server sorting/partition memory; choose input limits,
indexes and cancellation deadlines. PostgreSQL IGNORE NULLS and FROM LAST are not
implied by these helpers.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
