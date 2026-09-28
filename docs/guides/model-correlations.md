# Typed correlated subqueries

A correlated subquery refers to the current outer row. Foundry makes that relationship explicit in Go, preserving both scopes and the selected value type. These APIs use the same SELECT AST, PostgreSQL compiler and codecs as [ordinary subqueries](model-subqueries.md). The [consumer fixtures](../../tests/fixtures/consumer/correlations/) demonstrate their public usage, with nullable joined inputs in a separate compilation package.

## Find users with matching orders

```go
users, orders := models.QueryUsers(), models.QueryOrders()
link := query.Correlate(users, orders)
u := models.UserFieldsAt(query.OuterScope(link, users.Scope()))
o := models.OrderFieldsAt(query.InnerScope(link, orders.Scope()))

matching := link.Where(o.BuyerID.EqColumn(u.ID), o.TotalCents.Gte(1000))
buyers, err := users.Where(matching.Exists()).All(ctx, db)
```

`matching.Exists()` produces a user predicate. It cannot filter an order query. `matching.Exists().Not()` selects users without matching orders. The subquery executes within the outer SQL statement; application code does not first collect IDs or issue a query per user.

`Correlate` captures the inner query's filters and window. The outer argument declares its field scope; outer filters still belong on the actual outer query. Use `OuterScope` and `InnerScope` to bring generated field sets into the combined scope. Unadapted user or order predicates cannot be supplied to `matching.Where`.

`EqColumn` and `NeColumn` compare compatible column value types in the same scope. Numeric, text and other ordered fields also expose `LtColumn`, `LteColumn`, `GtColumn` and `GteColumn`; scalar enums do not gain range operators. IDs remain model-owned. SQL NULL comparisons remain unknown, so nullable keys do not match through equality simply because both are absent.

## Select a value per outer row

```go
counts := query.SelectCorrelatedValue(matching, o.ID.Count().Value())
perUser, err := query.SelectValue(users, query.CorrelatedScalarQuery(counts)).
    OrderBy(models.UserFields().Email.Asc()).All(ctx, db)
```

The output is `[]value.Nullable[int64]`. This particular `COUNT` has one result row per user, including a present zero for no matching orders. A scalar subquery in general can return no row, which becomes NULL, so its type retains nullability. More than one row returns PostgreSQL SQLSTATE 21000. Foundry never silently inserts `LIMIT 1`; add an explicit order and limit when choosing one row is intended.

For an already nullable expression, such as `o.TotalCents.Sum().Value()`, use `CorrelatedScalarNullableQuery`. It preserves one nullable layer and the exact decimal codec. Correlated scalars can fill compatible fields in a [declared projection](model-projections.md), retaining a user's identity beside the computed value.

`SelectCorrelatedValue` supports `Where`, `GroupBy`, `Having`, `OrderBy`, `Limit`, `Offset` and `Exists`. Its output remains bound to the outer scope. It has no standalone `Compile`, `All` or ordinary `ValueQuerySource` interface: executing it without the required outer row is a Go type error.

Typed membership retains that boundary:

```go
buyerIDs := query.SelectCorrelatedValue(matching, o.BuyerID.Value())
selected, err := users.Where(models.UserFields().ID.InCorrelatedQuery(buyerIDs)).
    All(ctx, db)
```

Use `InNullableCorrelatedQuery` for a nullable inner value of the same base type. NULL and negation follow the [same membership semantics](model-subqueries.md#select-one-typed-value) as ordinary subqueries.

## Aliases, nesting and joins

Self-correlation requires distinct [typed aliases](model-joins.md) and SQL names for the repeated model. Any aliased model or complete report can be an inner source. A filter/window applied before `As` remains inside that derived source; it limits the source globally. A limit on the correlated selection applies per outer row.

A containing `CorrelatedSource` can be the outer argument of another `Correlate`. Its scope includes both parent and grandparent sources. Bring each required field through the corresponding `OuterScope` calls. Nested references retain all enclosing scope and grouping checks. Ordinary, uncorrelated subqueries and derived FROM sources do not inherit this visibility.

Both outer and inner sources can be joins. Use `OuterNullableScope` or `InnerNullableScope` with generated `ModelNullableFieldsAt` accessors when a join can make that record absent. These scopes cannot be passed to the non-nullable field accessor, and already nullable fields stay singly nullable.

Correlated predicates also compose with scoped model updates/deletes and related-model filters. When a many-to-many loader assigns framework aliases, it rewrites the captured outer references through nested correlations while preserving independent inner sources. Applications do not repeat those aliases.

## Grouping and failures

When a scalar or HAVING subquery references a grouped outer query, each referenced outer column must be explicitly grouped. WHERE correlations run before grouping. The compiler carries these requirements through nested correlations. Foundry currently does not infer functional dependencies from primary keys; declare all referenced grouping fields.

Aggregates inside a correlation must use fields local to that SELECT. Aggregating only an outer column can change which SQL query owns the aggregate, so Foundry rejects it rather than silently altering the result shape. See PostgreSQL's [aggregate expression rules](https://www.postgresql.org/docs/18/sql-expressions.html#SQL-SYNTAX-AGGREGATES).

Compilation rejects zero/invalid descriptors, mismatched runtime aliases, shadowed source names, out-of-scope columns, invalid grouping and negative windows. SQL binding and expression/nesting budgets are shared across the full statement. Correlation does not introduce raw SQL strings or bypass codecs. Incoming values, actual database constraints and authorization still require runtime validation.

Execution retains the caller's context and executor. Canceled requests do not reach the executor; decoding or database failures discard collected output. A scalar cardinality error aborts its current PostgreSQL transaction scope; use a savepoint if the enclosing transaction must continue. Ordinary [write outcome rules](model-writes.md#transaction-ownership-and-failures) still apply.

[Relationship existence filters](model-relationship-filters.md) provide the simpler `WhereHas`/`WhereDoesntHave` path for declared relationships, including nested and pivot conditions with automatic aliases. [Milestone 06](../../blueprint/06-relations-and-advanced-queries.md) records delivered advanced-query APIs and remaining contracts. The master roadmap owns verification status.

[Correlated complete records and lateral joins](lateral-joins.md) retain these outer/inner scopes through full models and generated report selections. They cannot execute independently or enter ordinary uncorrelated sources.
