# Typed field computations

<!-- compiled-examples -->

Declare columns once with `orm.Define` and the appropriate typed constructors.
Use package functions from `database/orm/expr` to construct optional computations;
columns do not need a separate summary-method family for each owner/value pair.

| Purpose | Function |
| --- | --- |
| Row operand / bound parameter | `expr.Value(column)` / `expr.Param(column, value)` |
| Select a row operand | `expr.AsSelected(expr.Row(row))` |
| Count non-NULL / distinct non-NULL | `expr.CountField(column)` / `expr.CountDistinctField(column)` |
| Ordered extrema | `expr.Min(column)` / `expr.Max(column)` |
| Exact summaries | `expr.Sum(column)` / `expr.Avg(column)` |
| Floating summaries | `expr.SumFloat(column)` / `expr.AvgFloat(column)` |
| Interval summaries | `expr.SumInterval(column)` / `expr.AvgInterval(column)` |

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/expressionqueries/expressions_test.go):

<!-- go-source: tests/fixtures/consumer/expressionqueries/expressions_test.go -->
```go
orders := models.Orders.Query()
total := expr.Sum(orm.NumberExpr(models.Orders.TotalCents))
computed := expr.CoalesceValue(expr.AsSelected(total), expr.AsSelected(expr.ParamOf(total, decimal.FromInt64(0))))
```

`expr.Count[S]()` counts rows. Alias operands use `expr.At(scope, column)`;
handwritten result fields use `rows.At(scope, field)`. Aggregate `*At` constructors
accept an alias scope and a capable column. For a derived rows operand, use the
`*Row` or `*NullableRow` aggregate family, such as `expr.SumNullableRow`.

Capabilities come from the declaration, not only the underlying Go type. An enum
backed by a string does not gain text pattern or ordered comparisons. A derived
field must retain the required capability and codec; invalid or foreign metadata
fails before execution. The same checks apply to adapters such as `TextRow`,
`OrderRow` and `CompareRow`.

See [result types and empty input](model-aggregates.md#loading-and-result-types),
[conditional expressions](conditional-expressions.md) and
[scalar calculations](scalar-calculations.md).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
