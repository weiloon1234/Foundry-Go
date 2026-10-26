# Typed conditional expressions

<!-- compiled-examples -->

`database/orm/expr` preserves the input scope, value type and evaluation phase.
Use `expr.Value(column)` for a row operand, `expr.Row(row).Value()` or a computed
expression's `.Value()` for selection, and `rows.MapSelected` for a selected result.
Aggregates and windows cannot become WHERE predicates.

## Row values and NULL handling

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/expressionqueries/expressions_test.go):

<!-- go-source: tests/fixtures/consumer/expressionqueries/expressions_test.go -->
```go
u := models.Users
display := expr.Coalesce(expr.Value(u.Nickname), expr.Value(u.Email))
choice := expr.When(expr.ModelPredicate(u.Age.Gt(20)), expr.Param(u.Email, "older"))
label := choice.When(expr.ModelPredicate(u.Age.Eq(20)), expr.Value(u.Email)).Else(expr.Param(u.Email, "unknown"))
statement, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(expr.Where(models.Users.Query(), display.Ne(""))), label.Value()), label.Asc())))
```

`expr.Coalesce` takes a nullable value and a required fallback with the same base
type. It removes one nullable layer, retaining empty strings and numeric zero.
`CoalesceNullable` retains one layer. `NullIf` adds one nullable layer to matching
required inputs; `NullIfNullable` does not nest wrappers. `NullableRow` explicitly
widens a required row value. `NullFor` uses an exemplar's codec to construct NULL.
For generic nullable expressions, literal comparisons take a present
`value.Nullable[V]` (for example, `value.Of(v)`). A SQL NULL literal in equality
or membership is rejected; use `IsNull`/`IsNotNull` to test absence. Dedicated
nullable ordered/text adapters accept the present base value directly.

## CASE branches

`expr.When` accepts an `expr.Predicate[S]`; adapt a column predicate with
`expr.ModelPredicate`. Every branch must have the same owner and value type.
Complete the builder with `Else`, or `ElseNull` for required branches. An unfinished
builder cannot be selected. A NULL condition continues to the next branch.
Already-nullable branches require an explicit nullable `Else`. A zero builder
with no branches fails validation even if an ELSE is supplied.

## Selected aggregates and windows

`CoalesceValue`, `CoalesceNullableValue`, `NullIfValue`, `NullIfNullableValue` and
`WhenValue` accept selected expressions. `WhenValue` requires
`expr.HavingPredicate[S]`. `expr.Grouped` lifts a row predicate, but every referenced
column must still satisfy the enclosing SELECT's grouping rules. A window cannot
be hidden inside a conditional to make it legal in HAVING.

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/expressionqueries/expressions_test.go):

<!-- go-source: tests/fixtures/consumer/expressionqueries/expressions_test.go -->
```go
orders := models.Orders.Query()
total := expr.Sum(models.Orders.TotalCents)
computed := expr.CoalesceValue(total.Value(), total.Param(decimal.FromInt64(0)).Value())
```

## Bound parameters and codecs

`expr.Param(column, value)` captures the column's present value type and codec.
Computed `.Param` retains its expression's ownership errors. Nullable aggregates
accept a present fallback, as above. Encoding occurs during construction; encoded
buffers are owned, and encoding errors are returned before execution even when
SQL would not take that branch.

The compiler binds parameters and casts them using the codec's parameter type.
Custom codecs need explicit parameter-type metadata. All-parameter CASE branches
therefore retain numeric, temporal or other declared types instead of becoming
text. CASE does not suppress planning-time or earlier aggregate errors.

See [computed keys](computed-query-keys.md), [arithmetic and text](scalar-calculations.md)
and [typed JSON paths](json-models.md) for composition.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).

## Parameters without an exemplar

`expr.Constant[S](codec, value)` snapshots and binds a value with an explicit codec
and scope; it never interpolates SQL. `expr.Row(expr.At(scope, column)).Param(v)`
uses the scoped expression's projection type, including its nullable wrapper.
`Row` cannot demote selected or aggregate values into row predicates. The original
column-based `expr.Param(column, v)` continues to use its comparison input type.
See the compiled [CASE and scoped parameter examples](orm-migration.md#pilot-expressions-and-row-declarations).
