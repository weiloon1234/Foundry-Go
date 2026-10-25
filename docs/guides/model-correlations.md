# Typed correlated subqueries

<!-- compiled-examples -->

`rows.Correlate(outer, inner)` explicitly permits an inner SELECT to reference
its outer row. Lift each side through `rows.OuterScope` and `rows.InnerScope`;
`expr.At` then constructs operands in the combined correlation scope.

## Find users with matching orders

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/correlations/correlations_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/correlations/correlations_postgres_test.go -->
```go
link := rowset.Correlate(rowset.From(models.Users.Query()), rowset.From(models.Orders.Query()))
buyer := rowset.OuterScope(link, rowset.ModelScope(models.Users.Query()))
order := rowset.InnerScope(link, rowset.ModelScope(models.Orders.Query()))
matched := link.Where(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(buyer, models.Users.ID)))
counts := rowset.SelectCorrelatedScalar(matched, expr.CountFieldAt(order, models.Orders.ID).Value())
```

The `rowset` import is `database/orm/rows`. `matched.Exists()` is a predicate of
the outer user scope. Apply it with `expr.Where`; use `expr.Not` for NOT EXISTS.
This is one SQL statement, not an application query per user. The outer argument
supplies scope; keep outer filters on the executing outer query.

## Select a value per outer row

`rows.SelectCorrelatedScalar` returns a value tied to its outer owner. Embed it
with `CorrelatedScalarQuery`, or `CorrelatedScalarNullableQuery` when the inner
value is already nullable. A scalar COUNT yields a present zero over empty input,
but the general scalar result stays nullable because a SELECT can yield no row.
Multiple rows cause a database cardinality error; no implicit limit is added.

`InCorrelatedQuery` and `InNullableCorrelatedQuery` retain exact value types and
outer ownership. `CorrelatedScalarRowQuery` and its nullable counterpart produce
outer row operands while keeping the inner aggregate/window boundary.

`GroupByCorrelatedScalar`, `OrderCorrelatedScalar` and the correlated DISTINCT ON
adapters are package functions. Record projections use `ProjectCorrelated` with
a handwritten rows schema; `HavingCorrelatedRecord` and `DistinctCorrelatedRecord`
retain that record's outer owner. Correlated sources cannot execute independently.

## Aliases, nesting and joins

Self-correlation requires distinct alias tags and SQL names. An inner alias's
existing limit is global to that derived input; a limit on the correlated
selection is per outer row. A containing correlation can be the outer scope of
a nested one; lift parent/grandparent references explicitly at each level.

`OuterNullableScope`/`InnerNullableScope` preserve missing join sides. Use
`expr.AtNullable` or the corresponding rows field helper; do not erase nullability.
`expr.RelationFilter` adapts expression predicates for a declared relation while
validating both relation owners. Applications do not repeat loader aliases.

## Grouping and failures

Outer references from selected/HAVING subqueries must satisfy outer grouping;
WHERE runs before grouping. Aggregating only an outer column is rejected because
it would change the SELECT owning that aggregate. Alias shadowing, invalid
descriptors and illegal windows fail before execution.

Bindings, CTE discovery and compiler budgets span the statement. Decode failures
discard collected output; database cardinality errors require ordinary transaction
or savepoint recovery. See [lateral joins](lateral-joins.md) for correlated complete
records, and [transaction correlations](transaction-correlations.md) for locks.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
