# Declared query projections

A projection returns a complete, separately declared Go record containing the fields a read needs. Foundry generates its selection type and decoder; the shared query compiler selects only those expressions. The [consumer declarations](../../tests/fixtures/consumer/reports/projections.go) and [PostgreSQL acceptance](../../tests/fixtures/consumer/projectionqueries/projections_postgres_test.go) demonstrate partial reads, grouped reports and scalar summaries.

## Declare the result

Annotate an exported, non-generic struct with `//foundry:projection`. Every field must be exported, non-embedded and supported by the [database codecs](database-codecs.md). Imported generated enums and model-owned IDs preserve their concrete types.

```go
//foundry:projection
type UserSummary struct {
    ID       model.ID[models.User]
    Email    string `foundry:"column=contact_email"`
    Nickname value.Nullable[string]
    Status   models.Status
}
```

Projection declarations have no table or primary key. Field names default to snake case; `column` sets the SQL output alias, independently of the source column name. Ignored fields and database defaults are rejected: every result field needs a selection. Ordinary tags such as `json` remain application declarations. Persistence models do not automatically become public response DTOs.

Run the ordinary [generation command](model-generation.md#commands-and-ownership) from the consumer module. It discovers projections alongside models and enums, including dependencies on generated types in another selected package. The same deterministic publication, recovery and stale-output checks apply.

### Public report DTOs

The explicit `//foundry:projection dto=true` opt-in passed milestone 19
verification. It emits the projection mapping, `YourRowJSON()` contract and
`YourRowValidationFields()` typed selectors together. The struct is the shared
source for a report's SQL result and its public JSON fields; ordinary projections
remain independent from transport contracts. See the [datatable consumer
declarations](../../tests/fixtures/consumer/reporting/models.go).

The opt-in applies the [ordinary DTO rules](http-dtos.md), including explicit JSON
field names and rejection of persistence `foundry` tags. Use the typed selection
to choose source expressions instead of adding a `foundry:"column=..."` alias to
this public DTO. Do not add a second `//foundry:dto` marker. Models remain excluded
from implicit public serialization. The [datatable guide](datatable.md) explains
how these shared selectors bind columns, filters and export cells.

## Select typed values

Generation supplies `UserSummarySelection[InputScope]` and `SelectUserSummary`. Each expression must match both its input scope and the declared destination type:

```go
f := models.UserFields()
summary := reports.SelectUserSummary(
    models.QueryUsers().OrderBy(f.Email.Asc()),
    reports.UserSummarySelection[models.User]{
        ID: f.ID.Value(),
        Email: f.Email.Value(),
        Nickname: f.Nickname.Value(),
        Status: f.Status.Value(),
    },
)
rows, err := summary.Where(f.Status.Eq(models.StatusActive)).All(ctx, db)
```

`rows` is `[]reports.UserSummary`. A different model's expression, a string expression assigned to an integer field, or nullable input assigned to a non-nullable result fails compilation. `query.Nullable(expression)` explicitly widens a non-nullable value to its nullable result type without changing SQL. It does not replace SQL NULL with a default.

The generated `ProjectUserSummary(source)` fluent builder infers its input scope and provides typed `SelectID`, `SelectEmail` and corresponding setters for every result field. Its `Query()` method builds the same projection query. This is particularly useful for [typed joins](model-joins.md), whose full generic scope names need not appear in consumer code. Every setter returns a new builder; a repeated setter replaces the field's expression. Complete selection coverage is checked before SQL for both construction styles.

Go permits omitted struct-literal fields, so incomplete selections fail validation before SQL execution. Generated mappings must cover each declared result field exactly once. The lower-level `ProjectionDefinition`, `NewProjectionField`, `Map` and `Project` APIs are explicit metadata boundaries; custom definitions must supply an accurate complete decoder.

Queries preserve source filters, ordering and limit/offset. Subsequent `Where`, `OrderBy` and `GroupBy` retain the input model's type. Projection queries expose no model write or key-lookup methods. A projection record cannot be passed as a generated model draft. Source eager-loading clauses and explicit relation-loading limits are rejected because projected records have no model loading pipeline.

## Grouped and scalar summaries

Generated numeric fields and typed [aggregate expressions](model-aggregates.md#loading-and-result-types) also provide `Value()`. Integer and decimal sums/averages return nullable exact decimals; counts return `int64`.

```go
f := models.OrderFields()
totals := reports.SelectBuyerTotals(
    models.QueryOrders(),
    reports.BuyerTotalsSelection[models.Order]{
        BuyerID: f.BuyerID.Value(),
        Total: f.TotalCents.Sum().Value(),
        Orders: query.Count[models.Order]().Value(),
        Average: f.TotalCents.Avg().Value(),
    },
).GroupBy(f.BuyerID.Group()).OrderBy(f.BuyerID.Asc())
rows, err := totals.All(ctx, db)
```

Every selected or ordered ordinary column must be explicitly grouped when the query contains aggregates or grouping. Foundry does not infer PostgreSQL functional dependencies from primary keys. Duplicate grouping columns and ungrouped selections fail before execution; aggregate expressions do not expose `Group`. `Where` filters input rows before grouping.

A result containing only aggregates needs no `GroupBy`. Over empty input, a scalar aggregate query still returns one record: counts are zero and sums/averages/extrema are SQL NULL. A grouped query with no input has no result rows. `Limit(0)` suppresses either kind of result.

## Filter and order groups

Aggregate comparisons produce `HavingPredicate[Input]`. They cannot be passed to model or projection `Where`. Keep row filters in `Where`, then qualify the computed groups with `Having`:

```go
qualified := totals.Having(query.HavingAnd(
    query.Count[models.Order]().Gte(2),
    f.TotalCents.Sum().Gt(decimal.FromInt64(4)),
))
rows, err := qualified.All(ctx, db)
```

Counts expose integer equality, membership and range comparisons. Numeric summaries and extrema compare concrete values of their result type; nullable results add `IsNull` and `IsNotNull`. Boolean existence supports equality and membership, without numeric range methods. For example, a sum over an integer field compares `decimal.Decimal`, and a float average compares `float64`. Passing a different model's condition or an incompatible value fails compilation.

Multiple `Having` arguments/calls are combined with AND. `HavingAnd`, `HavingOr` and `Not` preserve expression grouping and model ownership. Use `query.Grouped(fieldPredicate)` to include an ordinary grouped column in these conditions. Every column in such a predicate must be in `GroupBy`; ungrouped references fail validation before SQL. These operations follow PostgreSQL's [group filtering order](https://www.postgresql.org/docs/18/sql-select.html#SQL-HAVING).

Nullable comparisons accept concrete values. Use explicit NULL predicates instead of passing a nullable wrapper. SQL NULL comparisons remain unknown, including after `Not`; they do not retain a group. An empty `In()` matches no groups. See PostgreSQL's [NULL comparison behavior](https://www.postgresql.org/docs/18/functions-comparison.html). HAVING can remove the otherwise-present single scalar aggregate record over empty input; `Count` and `Exists` observe the filtered result.

Projection `OrderBy` accepts ordinary field orders and typed aggregate/expression orders in the supplied sequence:

```go
ranked := reports.SelectBuyerTotals(models.QueryOrders(), selection).
    GroupBy(f.BuyerID.Group()).
    OrderBy(f.TotalCents.Sum().Desc(), f.BuyerID.Asc())
```

Here `selection` is the complete `BuyerTotalsSelection[models.Order]` shown above. Source orders and later `OrderBy` calls are appended; start with an unordered source when choosing a new primary ranking. Supply a stable tie-breaker when paginating. PostgreSQL's default NULL ordering applies. Model queries still accept only their field-based `Order[Model]`; aggregate ordering requires a projection. An aggregate used only in ordering still makes the SQL query grouped and requires ordinary selected columns to appear in `GroupBy`.

## Execution and failures

- `All` collects complete records and discards the collection on decoding failure.
- `Each` streams records to a callback and closes rows on callback error, decoding failure or cancellation. Already-delivered callback effects cannot be rolled back by collection semantics. Consumers choose a limit or streaming when the result could be large.
- `First` returns `value.Optional[Result]`; `RequireFirst` returns `database.NotFound` when absent. Both honor offset and an existing zero limit. Supply ordering when row choice matters.
- `Count` counts the selected result window, including grouping, HAVING and limit/offset. It counts groups for a grouped query and one row for an ungrouped aggregate result over empty input when HAVING retains that row.
- `Exists` tests that same result window. Neither `Count` nor `Exists` decodes projected values or executes model lifecycle behavior.
- `Paginate` adds a total and numbered metadata; `SimplePaginate` uses a lookahead row without counting. Both require explicit ordering and preserve complete projection types. Their [shared pagination contract](model-pagination.md#projected-combined-and-single-value-results) covers grouped totals, nested limits and failure disposal.

Queries use the supplied context and executor; transactions remain caller-owned. Result codecs validate database values, including enum membership. Each successful decoder yields a fresh complete record. Separate statements need suitable transaction isolation when a common snapshot is required.

[Typed joins and self-join aliases](model-joins.md) supply scoped fields and compiler-checked outer nullability for these projection records. `SelectRecord` can reuse the complete decoder of a preserved model or report scope without individual field mappings. [Derived records and subqueries](model-subqueries.md), [relationship filters](model-relationship-filters.md), [explicit correlations](model-correlations.md), [nonrecursive CTEs](model-ctes.md), [recursive CTEs](model-recursive-ctes.md), [set operations](model-set-operations.md) and [window expressions](model-windows.md) compose through the same compiler. Delivered projections include field grouping, typed HAVING, aggregate ordering, scalar summaries, numbered/simple pagination and [result cursor pagination](result-cursor-pagination.md). [Typed calculations](scalar-calculations.md) and [value comparisons](value-comparisons-and-joins.md) compose fields and computed values without raw SQL. The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records verification status for the transaction-required projection variants.
