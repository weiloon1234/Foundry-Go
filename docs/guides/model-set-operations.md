# Typed set operations

Combine complete model or projection results with `Union`, `UnionAll`, `Intersect`, `IntersectAll`, `Except` and `ExceptAll`. Both inputs must return the same concrete Go record type; projection inputs may come from different model or join scopes. The resulting `SetQuery[Record]` exposes complete read results and a new typed field scope. The [independent consumer fixture](../../tests/fixtures/consumer/setqueries/sets_postgres_test.go) exercises these contracts against PostgreSQL.

## Combine models

```go
u := models.UserFields()
combined := models.QueryUsers().Where(u.Age.Lte(21)).
    Union(models.QueryUsers().Where(u.Age.Gte(21)))

fields := models.UserFieldsAt(combined.Scope())
users, err := combined.Where(fields.Status.Eq(models.StatusActive)).
    OrderBy(fields.Email.Asc()).All(ctx, db)
```

`combined` is `query.SetQuery[models.User]`; `users` is `[]models.User`. The input fields belong to `models.User`. Combined fields belong to `query.Set[models.User]`, preventing an input predicate from accidentally entering the output scope. Ordering and predicates retain their usual field-specific operators, model IDs and value types.

`Union` removes duplicate complete SQL rows. It does not deduplicate by primary key alone. `UnionAll` preserves duplicates. Intersection keeps common rows, and difference keeps left rows absent from the right. Their `All` forms retain multiplicity: intersection uses the smaller count, while difference subtracts right counts from left counts. NULL values participate in duplicate matching under the database's set semantics. [PostgreSQL set operations](https://www.postgresql.org/docs/18/queries-union.html)

Top-level `query.Union(left, right)` and the corresponding functions for the other operations accept complete record sources, including [CTE descriptors](model-ctes.md). Fluent methods are available on model, projection and set queries. These operations build queries without database I/O.

## Keep input and output windows separate

Filters, ordering, limits and offsets on an input execute before combination. Calls on the set query apply afterward. Foundry parenthesizes every operand and preserves the composition tree, so chaining mixed operations does not depend on SQL operator precedence.

```go
left := models.QueryUsers().OrderBy(u.Age.Desc()).Limit(1)
right := models.QueryUsers().OrderBy(u.Age.Asc()).Limit(1)
combined := left.UnionAll(right)
fields := models.UserFieldsAt(combined.Scope())
users, err := combined.OrderBy(fields.Age.Asc()).Limit(2).All(ctx, db)
```

Order the final result explicitly when order matters. Input ordering does not promise final ordering. `First` returns an optional record and `RequireFirst` returns a record or `database.NotFound`; both honor the result window, including a zero limit. Set queries do not add implicit primary-key ordering. `Count` counts the selected window after combination and duplicate handling.

`Paginate` and `SimplePaginate` return typed pages of the combined result, preserving each operand's limits and duplicate behavior. They require explicit result ordering and reject a preexisting limit/nonzero offset on the combined result. See the shared [pagination contract](model-pagination.md#projected-combined-and-single-value-results).

Set results expose read operations. They do not expose model mutations, key lookup or eager loading, and set inputs cannot contain eager-loading clauses. Load relationships explicitly on returned model slices through the ordinary model query when needed. To constrain a write, select typed IDs from a set and use that subquery in the normal model builder.

## Combine declared reports

Use a shared [projection record](model-projections.md) when inputs represent different domain models. For example, the consumer's order and user queries each select a complete `reports.BuyerTotals`, with different input scopes:

```go
combined := orders.UnionAll(people)
fields := reports.BuyerTotalsFieldsAt(combined.Scope())
rows, err := combined.Where(fields.Total.Gte(decimal.FromInt64(20))).
    OrderBy(fields.Total.Asc()).All(ctx, db)
```

Here `orders` and `people` are the complete projection queries declared in the consumer fixture; they are not table-name strings. Every selected field must match `BuyerTotals`. Generated output names, exact decimal values, nullable fields and enums survive combination.

Set queries also implement the existing typed projection-source boundary. `ProjectResult(combined)` can select or aggregate their output fields. `As[Tag](combined, "alias")` introduces the result into a join; its filters and window stay within that source. CTEs can consume set queries, and either set input can reference CTEs. Shared CTE descriptors emit once in dependency order across the statement.

## Combine single values

Single-value queries have the same fluent operations and return `ValueSetQuery[V]`. Their sealed single-column contract remains available for typed membership and scalar subqueries:

```go
o := models.OrderFields()
buyers := query.SelectValue(models.QueryOrders(), o.BuyerID.Value())
combined := buyers.Where(o.TotalCents.Lt(3)).
    Union(buyers.Where(o.TotalCents.Gte(3)))

users, err := models.QueryUsers().Where(u.ID.InQuery(combined)).All(ctx, db)
```

`combined` returns user IDs; order IDs cannot be substituted. Nullable and non-null values cannot be silently combined. Widen an input explicitly with `query.Nullable(expression)` when the shared result should be nullable, then use the nullable membership/scalar APIs where appropriate.

`combined.Value()` is the output expression for `OrderBy(combined.Value().Asc())` or `Desc()`. Further `Limit` and `Offset` calls preserve the single-value boundary. Apply field-specific input filters before combination, or use a declared report with generated output fields for richer output filtering.

## Execution and validation

Sets, models and projections share the SELECT AST/compiler, codec contracts and parameter budget. Projection and set results share collection/streaming execution: `All` discards partial output on failure, while `Each` closes rows on callback errors or cancellation. A callback can already have performed work before a later row fails; streaming does not roll back its external effects.

Wrong record/value owners and input/output field scopes fail compilation. Invalid zero/nil inputs, inconsistent record layouts, unsupported query options, invalid bound values and excessive AST/parameter bounds fail before executor access. Database type compatibility and equality semantics still depend on the physical schema and codecs at explicit declaration boundaries.

[Typed recursive CTEs](model-recursive-ctes.md) combine an anchor and an explicitly owned recursive step through this same set compiler. Ordinary set operations do not grant permission to use a working-table reference outside its owning step.
