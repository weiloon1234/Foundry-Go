# Typed aggregate filters

<!-- compiled-examples -->

An aggregate's `Filter` restricts its own contributing rows. Source WHERE filters
all calculations; `rows.Having` filters completed groups. Adapt model predicates
with `expr.ModelPredicate`; aliased operands already produce expression predicates.

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/projectionqueries/aggregate_contracts_test.go):

<!-- go-source: tests/fixtures/consumer/projectionqueries/aggregate_contracts_test.go -->
```go
predicate := expr.ModelPredicate(models.Orders.TotalCents.Gt(0))
total := expr.Sum(models.Orders.TotalCents).Filter(predicate)
count := expr.Count[models.Order]().Filter(predicate)
exists := expr.Exists[models.Order]().Filter(predicate)
selected := rows.Select(rows.From(models.Orders.Query()), reports.OrderSummaries,
	rows.MapSelected(reports.OrderSummaries.Total, total.Value()),
	rows.MapSelected(reports.OrderSummaries.Count, count.Value()))
got, err := rows.Compile(rows.Having(selected, expr.HavingAnd(count.Gt(0), total.Gt(decimal.FromInt64(1)), exists.Eq(true))))
```

Chained `Filter` calls append with AND, leaving previous descriptors unchanged.
An empty call adds no conditions; an invalid zero predicate fails compilation.
Use `expr.And`, `expr.Or` and predicate `Not` for explicit logical composition.
Only true rows contribute; false and SQL unknown do not.

## Windows and relations

Apply the filter before `expr.Over(aggregate, window)`. It changes neither window
membership nor output cardinality. A frame with no contributing input yields
zero count, false existence or NULL summary. Grouped window filters can inspect
only grouped columns. Ranking functions have no aggregate filter.

Use the same descriptor with `expr.Related(relation, aggregate)` and a typed
relation value slot. Target filters belong to the target; pivot filters belong to
`orm.Pivot(relation)`. Filtering a measure does not reduce the full relationship
input used for cardinality checks. See [relation aggregates](model-aggregates.md).

## NULLs and validation

Counts stay `int64`; integer sums/averages stay nullable exact decimals. Empty or
all-NULL filtered summaries stay NULL. A filter alone never removes an existing
group. There is no general aggregate DISTINCT API beyond `CountDistinctField`.

Filters preserve subquery/CTE discovery and explicit correlation. A measured
field must belong to the aggregate's SELECT. Count/Exists filters that capture
outer data must also reference their own input; outer-only aggregates are rejected
before execution. See the [compiled consumer](../../tests/fixtures/consumer/filterqueries/filters_postgres_test.go).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
