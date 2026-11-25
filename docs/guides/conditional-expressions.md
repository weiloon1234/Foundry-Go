# Typed conditional expressions

`database/orm/expr` preserves the input scope, value type and evaluation phase.
Use `expr.Value(column)` for a text row operand, `expr.AsSelected(expr.Row(row))` or a computed
`expr.AsSelected(expression)` for selection, and `rows.MapSelected` for a selected result.
Aggregates and windows cannot become WHERE predicates.

## Row values and NULL handling

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database/codec`, `database/orm`, `database/orm/expr`, `database/orm/rows`, `decimal`.

`models.User` is the application's model with an integer `Age` and text `Nickname` (nullable) and `Email`, and `models.Users` is its generated table. `LabelsByAge` selects a CASE label for each user with a display name, ordered by that label:

<!-- compiled-source: tests/fixtures/consumer/expressionqueries/guide_expressions.go#conditional-expressions -->
```go
func LabelsByAge() rows.Scalar[models.User, string] {
	u := models.Users
	display := expr.Coalesce(expr.Value(u.Nickname), expr.Value(u.Email))
	choice := expr.When(expr.ModelPredicate(orm.Gt(u.Age, 20)), expr.Param(u.Email, "older"))
	label := choice.When(expr.ModelPredicate(orm.Eq(u.Age, 20)), expr.Value(u.Email)).Else(expr.Param(u.Email, "unknown"))
	return rows.OrderScalar(rows.SelectScalar(rows.From(expr.Where(models.Users.Query(), expr.Ne(display, ""))), expr.AsSelected(label)), expr.Asc(label))
}
```

`expr.Coalesce` takes a nullable value and a required fallback with the same base
type. It removes one nullable layer, retaining empty strings and numeric zero.
`CoalesceNullable` retains one layer. `NullIf` adds one nullable layer to matching
required inputs; `NullIfNullable` does not nest wrappers. `NullableRow` explicitly
widens a required row value. `expr.ParamOf(expr.NullableRow(v), value.Null[T]())` uses the exemplar's codec for NULL.
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

`models.Order` is the application's model with an integer `TotalCents` column, whose `expr.Sum` is a nullable decimal. `TotalOrZero` selects that sum, or zero when it is NULL:

<!-- compiled-source: tests/fixtures/consumer/expressionqueries/guide_expressions.go#conditional-expressions-2 -->
```go
func TotalOrZero() expr.Selected[models.Order, decimal.Decimal] {
	total := expr.Sum(models.Orders.TotalCents)
	return expr.CoalesceValue(expr.AsSelected(total), expr.AsSelected(expr.ParamOf(total, decimal.FromInt64(0))))
}
```

## Bound parameters and codecs

`expr.Param(column, value)` captures the column's present value type and codec.
`expr.ParamOf(e, v)` retains its expression's ownership errors. Nullable aggregates
accept a present fallback, as above. Encoding occurs during construction; encoded
buffers are owned, and encoding errors are returned before execution even when
SQL would not take that branch.

The compiler binds parameters and casts them using the codec's parameter type.
Custom codecs need explicit parameter-type metadata. All-parameter CASE branches
therefore retain numeric, temporal or other declared types instead of becoming
text. CASE does not suppress planning-time or earlier aggregate errors.

See [computed keys](computed-query-keys.md), [arithmetic and text](scalar-calculations.md)
and [typed JSON paths](json-models.md) for composition.


## Parameters without an exemplar

`expr.Constant[S](codec, value)` snapshots and binds a value with an explicit codec
and scope; it never interpolates SQL. `expr.ParamOf(expr.Row(expr.At(scope, column)), v)`
uses the scoped expression's projection type, including its nullable wrapper.
`Row` cannot demote selected or aggregate values into row predicates. The original
column-based `expr.Param(column, v)` continues to use its comparison input type.

`Admin` is the application's model and `Admins` its generated table; the second function
aliases the table's query as a named scope and returns the parameter bound to that scope's column:

<!-- compiled-source: tests/fixtures/goway/migration/pilot2_test.go#pilot2-row-constant -->
```go
func enabledAdmins() rows.Scalar[Admin, bool] {
	enabled := expr.When(expr.Eq(expr.Value(Admins.Username), "active"),
		expr.Constant[Admin](codec.Bool[bool](), true)).Else(expr.Constant[Admin](codec.Bool[bool](), false))
	return rows.SelectScalar(rows.From(Admins.Query()), expr.AsSelected(enabled))
}

type adminScope struct{}

func scopedUsername() expr.RowExpression[rows.Alias[adminScope, Admin], string] {
	source := rows.As[adminScope](Admins.Query(), "admin")
	return expr.ParamOf(expr.Row(expr.At(rows.Scope(source), Admins.Username)), "Ada")
}
```
