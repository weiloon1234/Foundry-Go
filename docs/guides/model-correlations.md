# Typed correlated subqueries

<a id="agent-entry"></a>
## Agent entry

- [Match inner rows](#recipe-correlate-rows)
- [Select a correlated scalar](#recipe-correlated-scalar)

## Overview

`rows.Correlate(outer, inner)` explicitly permits an inner SELECT to reference
its outer row. Lift each side through `rows.OuterScope` and `rows.InnerScope`;
`expr.At` then constructs operands in the combined correlation scope.

<a id="recipe-correlate-rows"></a>
## Find users with matching orders

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database/orm/expr`, `database/orm/rows`.

`models.User` and `models.Order` are the application's models, an order holding its buyer in `BuyerID` and an integer `TotalCents`. The block builds the correlated sources and two scalar subqueries in one scope:

<!-- compiled-source: tests/fixtures/consumer/correlations/guide_correlations.go#model-correlations -->
```go
link := rows.Correlate(rows.From(models.Users.Query()), rows.From(models.Orders.Query()))
buyer := rows.OuterScope(link, rows.ModelScope(models.Users.Query()))
order := rows.InnerScope(link, rows.ModelScope(models.Orders.Query()))
matched := link.Where(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(buyer, models.Users.ID)))
counts := rows.SelectCorrelatedScalar(matched, expr.AsSelected(expr.CountFieldAt(order, models.Orders.ID)))
totals := rows.SelectCorrelatedScalar(matched, expr.AsSelected(expr.SumAt(order, models.Orders.TotalCents)))
```

`matched.Exists()` is a predicate of
the outer user scope. Apply it with `expr.Where`; use `expr.Not` for NOT EXISTS.
This is one SQL statement, not an application query per user. The outer argument
supplies scope; keep outer filters on the executing outer query.

<a id="recipe-correlated-scalar"></a>
## Select a value per outer row

`rows.SelectCorrelatedScalar` returns a value tied to its outer owner. Embed it
with `expr.AsSelected(rows.CorrelatedScalarRowQuery(selected))`, or
`expr.AsSelected(rows.CorrelatedScalarNullableRowQuery(selected))` when the inner value is
already nullable. A scalar COUNT yields a present zero over empty input,
but the general scalar result stays nullable because a SELECT can yield no row.
Multiple rows cause a database cardinality error; no implicit limit is added.

`reports` is the application's package of result schemas: `reports.UserOrderStats` is the result struct with `ID`, `Email`, an `int64` `Orders` and a nullable decimal `Total`, and `reports.UserOrderStatsRows` its `rows.Define` schema.

Inputs: `counts rows.CorrelatedScalar[models.User, models.Order, int64]`, `totals rows.CorrelatedScalar[models.User, models.Order, value.Nullable[decimal.Decimal]]`.

<!-- compiled-source: tests/fixtures/consumer/correlations/guide_correlations.go#embed-correlated-scalar -->
```go
u := models.Users
r := reports.UserOrderStatsRows
report := rows.OrderBy(rows.Select(rows.From(models.Users.Query()), r.Row,
	rows.Map(r.ID, expr.Value(u.ID)), rows.Map(r.Email, expr.Value(u.Email)),
	rows.MapSelected(r.Orders, expr.AsSelected(rows.CorrelatedScalarRowQuery(counts))),
	rows.MapSelected(r.Total, expr.AsSelected(rows.CorrelatedScalarNullableRowQuery(totals)))), expr.Asc(expr.Value(u.Email)))
```

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
