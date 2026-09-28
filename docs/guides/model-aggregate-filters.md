# Typed aggregate filters

`Filter` restricts the rows contributing to one aggregate. Use query `Where` to restrict every calculation's input, and `Having` to filter the completed groups. Generated predicates retain the aggregate's model or joined scope; neither column names nor raw SQL are needed.

The [independent consumer examples](../../tests/fixtures/consumer/filterqueries/filters_postgres_test.go) exercise grouped reports, windows, nullable results, CTEs, correlations and relation slots.

## Different calculations in one report

```go
o := models.OrderFields()
filtered := o.TotalCents.Sum().Filter(o.TotalCents.Gt(1))

rows, err := reports.SelectBuyerTotals(models.QueryOrders(),
    reports.BuyerTotalsSelection[models.Order]{
        BuyerID: o.BuyerID.Value(),
        Total:   filtered.Value(),
        Orders:  query.Count[models.Order]().Value(),
        Average: o.TotalCents.Avg().Filter(o.TotalCents.Lt(3)).Value(),
    }).
    GroupBy(o.BuyerID.Group()).
    Having(filtered.Gt(decimal.FromInt64(1))).
    All(ctx, db)
```

`Orders` counts all input orders for each buyer. `Total` sums only amounts above one, while `Average` includes only amounts below three. Each calculation keeps its existing result type and codec. Integer sums and averages remain nullable exact decimals; filtered counts remain `int64` and retain comparisons such as `Gt`.

Chained `Filter` calls append predicates with AND without changing the original aggregate. Use `query.Or`, `query.And` and predicate `Not` for composition. An empty `Filter()` is a no-op; an invalid zero predicate fails compilation of the SQL statement before execution. Query declarations remain immutable.

## Windows and relations

Apply the filter before `Over`:

```go
base := models.QueryOrders()
o := models.OrderFields()
window := query.WindowFor(base).
    OrderBy(o.TotalCents.Asc(), o.ID.Asc()).
    RowsBetween(query.UnboundedPreceding(), query.CurrentRow())

running := o.TotalCents.Sum().Filter(o.TotalCents.Gt(1)).Over(window)
values, err := query.SelectValue(base, running).
    OrderBy(o.TotalCents.Asc(), o.ID.Asc()).All(ctx, db)
```

Window filtering does not remove output rows or change the partition/frame. A frame with no qualifying inputs produces the aggregate's empty result. In a grouped query, window filters operate on grouped rows and may reference only grouped columns. `Filter` belongs to aggregate descriptors; ranking functions and completed window expressions do not expose it. PostgreSQL's unsupported distinct window aggregates remain rejected.

The same filtered aggregate can populate a generated relation slot:

```go
count := models.UserAggregates().OrderCount.Using(query.Related(
    models.UserRelations().Orders,
    query.Count[models.Order]().Filter(models.OrderFields().TotalCents.Gt(2)),
))
users, err := models.QueryUsers().With(count).All(ctx, db)
```

Handwritten `DefineAggregates` declarations may use this same composition. Target aggregates require target predicates; pivot aggregates require pivot predicates. A filter affects the measure, while relation cardinality checks still inspect the complete scoped relationship input. Existing loaded-state, batch and row-budget contracts apply.

## NULLs, subqueries and validation

Only rows whose predicate is true contribute; false and SQL unknown/NULL do not. An empty filtered count is zero and existence is false. Sums, averages and extrema return SQL NULL when no qualifying non-NULL value remains. Filtering does not remove a group that existed in the query input. These are PostgreSQL's [aggregate FILTER semantics](https://www.postgresql.org/docs/18/sql-expressions.html#SYNTAX-AGGREGATES).

Filters accept the existing typed row predicates, including supported subqueries and explicit correlations. CTE dependency discovery, parameter binding, alias qualification and resource bounds use the shared query infrastructure. Ordinary aggregate and window expressions cannot be passed as row predicates; a subquery can own its own calculations.

PostgreSQL can move an aggregate to an enclosing SELECT when all its argument/filter references belong there. Ordinary field aggregates in Foundry require a measured field in their own SELECT. For `Count`/`Exists` without a measured field, a filter referencing outer data must also reference the aggregate's own input; outer-only filters are rejected. References captured through nested correlations follow the same rule. This check does not claim to validate every PostgreSQL semantic constraint at Go compile time.

Fetched models and report rows remain native typed slices. Filtering an aggregate performs SQL work; ordinary Go operations on a returned slice perform no further database query.
