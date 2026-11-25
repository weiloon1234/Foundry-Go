# Typed aggregate filters

`expr.Filter(aggregate, predicates...)` restricts one aggregate's contributing rows. Source WHERE filters
all calculations; `rows.Having` filters completed groups. Adapt model predicates
with `expr.ModelPredicate`; aliased operands already produce expression predicates.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database/orm`, `database/orm/expr`, `database/orm/rows`, `decimal`.

`models.Order` is the application's model with an integer `TotalCents`, and `models.Orders` its generated table. `OrderSummary` is the result struct with a nullable decimal `Total` and an `int64` count field, and `OrderSummaries` its `rows.Define` schema. `FilteredOrderSummary` filters each aggregate to orders with a positive total and keeps groups where all three hold:

<!-- compiled-source: tests/fixtures/consumer/reports/guide_summaries.go#model-aggregate-filters -->
```go
func FilteredOrderSummary() rows.Query[models.Order, OrderSummary] {
	predicate := expr.ModelPredicate(orm.Gt(models.Orders.TotalCents, 0))
	total := expr.Filter(expr.Sum(models.Orders.TotalCents), predicate)
	count := expr.Filter(expr.Count[models.Order](), predicate)
	exists := expr.Filter(expr.Exists[models.Order](), predicate)
	selected := rows.Select(rows.From(models.Orders.Query()), OrderSummaries,
		rows.MapSelected(OrderSummaries.Total, expr.AsSelected(total)),
		rows.MapSelected(OrderSummaries.Count, expr.AsSelected(count)))
	return rows.Having(selected, expr.HavingAnd(expr.GtValue(count, 0), expr.GtValue(total, decimal.FromInt64(1)), expr.EqValue(exists, true)))
}
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
before execution. 
