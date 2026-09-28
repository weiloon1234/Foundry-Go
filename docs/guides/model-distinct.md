# Typed distinct reads

`Distinct()` removes duplicate **selected rows**. `DistinctOn(...)` selects one ordered row per combination of typed keys. Both compile through the shared PostgreSQL SELECT pipeline and preserve the declared result type. The [independent consumer](../../tests/fixtures/consumer/distinctqueries/distinct_postgres_test.go) exercises models, projections, scalar values, joins, CTEs, sets and correlations.

## Unique values and complete models

```go
u := models.UserFields()
statuses, err := query.SelectValue(models.QueryUsers(), u.Status.Value()).
    Distinct().All(ctx, db) // []models.Status

users, err := models.QueryUsers().Distinct().
    OrderBy(u.Email.Asc()).All(ctx, db) // []models.User
```

Distinctness considers every selected column. Complete models with different primary keys remain different rows even if their email addresses match. Use `SelectValue` or a declared projection when deduplicating a smaller result shape. SQL NULL values compare as duplicates for this operation, while hydration still returns `value.Nullable[T]`.

A model's `Distinct`/`DistinctOn` returns `ProjectionQuery[Model, Model]`: a read-only builder returning complete models through their generated decoder. It has `Where`, `OrderBy`, `First`, `RequireFirst`, `All`, `Each`, `Count` and `Exists`, and composes as a record source. Model mutation, key-lookup, eager-loading and model cursor methods are not exposed on this selection. An existing eager-loading clause is rejected; use explicit `Load` after collecting models. [Numbered/simple result pages](model-pagination.md) preserve distinct cardinality. [Model chunk helpers](model-chunks.md) apply to the model query, not this read-only selection.

For a joined model, apply distinctness to its complete selection:

```go
people := query.As[personAlias](models.QueryUsers(), "person")
orders := query.As[purchaseAlias](models.QueryOrders(), "purchase")
p, o := models.UserFieldsAt(people.Scope()), models.OrderFieldsAt(orders.Scope())
joined := query.InnerJoin(people, orders, query.On(p.ID, o.BuyerID))
scope := query.LeftScope(joined, people.Scope())
fields := models.UserFieldsAt(scope)

buyers, err := query.SelectRecord(joined, scope).Distinct().
    OrderBy(fields.Email.Asc()).All(ctx, db)
```

Define the alias tags as distinct ordinary Go types. Repeated purchase rows no longer duplicate the selected buyer, and the returned slice still contains complete `models.User` values. Nullable join sides require declared nullable projections, as described in the [join guide](model-joins.md).

## One ordered row per group

```go
o := models.OrderFields()
largestOrders, err := models.QueryOrders().
    DistinctOn(o.BuyerID.Group()).
    OrderBy(o.BuyerID.Asc(), o.TotalCents.Desc(), o.ID.Asc()).
    All(ctx, db) // []models.Order, one order per buyer
```

`Group()` supplies a scope-owned field or computed row descriptor; `DistinctOn` does not introduce string column lists or aggregate grouping. Its keys need not be selected. Begin ordering with the distinct keys, followed by the desired row precedence and a unique tie-breaker. Without a complete ordering, the chosen row is unspecified. PostgreSQL accepts a permutation or shortened prefix of the keys; an unrelated ordering expression can only follow after all distinct keys have appeared. Foundry validates that boundary before execution. These selection semantics follow [PostgreSQL's DISTINCT documentation](https://www.postgresql.org/docs/18/sql-select.html#SQL-DISTINCT).

`DistinctOn` requires at least one declared key, rejects duplicate/zero keys and preserves the input scope. Keys from another model or alias fail Go compilation; undeclared or mismatched concrete aliases fail query compilation. Grouped queries must also satisfy the existing grouped-column rules. [Computed query keys](computed-query-keys.md) add `RowExpression.Group()` and selected `Expression.Key()` values through `DistinctOnValues`, including aggregate/window results.

## Ordering, counting and composition

For ordinary `Distinct`, every ordering expression must match a selected expression, including its encoded parameter values. The compiler refers to the matched output position, preserving selected scalar/aggregate semantics without binding an equivalent expression again. Ordering by an unselected field fails before execution. Expressions and parameter comparison work use bounded compiler budgets. To sort by different output criteria, first expose the distinct result through a typed alias and sort its generated fields in an outer query.

Distinctness applies after row filtering and grouping/HAVING, and before limit/offset. `Count` counts that complete selected window, preserving selected columns rather than collapsing the inner query to a constant. `Exists` and `First` cap the result at one row while retaining distinctness and offset; `Limit(0)` stays empty. Read-only first-row methods add no implicit ordering. `All` returns an ordinary typed slice and discards collected output on failure. `Each` shares the existing streaming, cleanup and cancellation behavior.

`Distinct` and `DistinctOn` replace one another on the same immutable builder. Derivation leaves the original unchanged. To deduplicate an already limited input, alias it first; adding `Distinct()` to the same builder deduplicates before its existing limit.

Complete distinct results compose with [CTEs](model-ctes.md), [recursive CTEs](model-recursive-ctes.md), [set operations](model-set-operations.md), joins and subqueries. `SetQuery.Distinct`/`DistinctOn` apply to the combined record result. `ValueQuery.Distinct` and `ValueSetQuery.Distinct` retain their single-column codec and can enter typed membership/scalar APIs. `CorrelatedValueQuery.Distinct`/`DistinctOn` retain required outer ownership; they cannot execute independently.

No raw SQL is needed for these operations. Database support for comparing a stored type, collation behavior and runtime constraints remain PostgreSQL checks; typed declarations do not promise that every possible database type has a SQL equality operator.
