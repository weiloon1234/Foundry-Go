# Typed conditional expressions

Computed values keep their query scope and exact Go result type. Generated fields implement `RowValue[Scope, Value]`, including nullable fields whose value is `value.Nullable[T]`. Row computations return `RowExpression`; its `Value()` promotes it to the existing selected `Expression` used by projections. Aggregates and window expressions cannot be passed as row values.

The [independent consumer examples](../../tests/fixtures/consumer/expressionqueries/) exercise these public APIs. Result queries still return native Go slices and share ordinary decoding, streaming and failure behavior.

## Row values and NULL handling

```go
u := models.UserFields()
display := query.Coalesce(u.Nickname, u.Email)

names, err := query.SelectValue(
    models.QueryUsers().Where(display.Ne("")),
    display.Value(),
).OrderBy(display.Asc()).All(ctx, db)
```

`Coalesce` requires a nullable input and a non-nullable fallback of the same base type, producing a non-nullable result. An empty string or numeric zero is retained. `CoalesceNullable` accepts nullable inputs and keeps one nullable result layer. `NullIf` makes equal non-nullable inputs into SQL NULL; `NullIfNullable` compares already-nullable inputs without nesting wrappers. Comparisons follow the database's equality and collation rules. See PostgreSQL's [conditional expression semantics](https://www.postgresql.org/docs/18/functions-conditional.html).

`NullableRow(value)` explicitly widens a row value while preserving its SQL evaluation. `NullFor(value)` creates a typed NULL using a non-nullable exemplar's codec without reading the exemplar's SQL value. Nested nullable wrappers are rejected when compiling these expressions.

Computed row values expose `Eq`, `Ne`, `In`, `IsNull` and `IsNotNull`. Literal comparisons accept the expression's exact result type: a nullable computation uses `value.Of(v)` for a present comparison value. Literal SQL NULL must use `IsNull`/`IsNotNull`; equality or membership containing a NULL literal is rejected. Model fields retain their established comparison signatures. Use `Value()` for selection and `Asc`/`Desc` for projection ordering.

## CASE branches

```go
u := models.UserFields()
label := query.When(u.Age.Gt(20), u.Email.Param("older")).
    When(u.Age.Eq(20), u.Email.Param("twenty")).
    Else(u.Email.Param("younger"))

labels, err := query.SelectValue(models.QueryUsers(), label.Value()).All(ctx, db)
```

`When` starts a row CASE; additional branches preserve the same owner and result type. The first true condition supplies the value. False and SQL unknown continue to the next branch. `Else` completes the builder with the same result type, while `ElseNull` adds one nullable layer. An unfinished builder cannot be selected. A zero builder may acquire its first branch through `When`; completing a builder with no branches is invalid.

Already-nullable branches use an explicit nullable `Else`, such as a nullable field or `NullFor(nonNullableValue)`. Calling `ElseNull` on an already-nullable result is rejected. Branches and parameters are captured without mutating earlier builders.

## Selected aggregates and windows

The corresponding selected-value operations are `CoalesceValue`, `CoalesceNullableValue`, `NullIfValue`, `NullIfNullableValue` and `WhenValue`. They accept existing `Expression` values, including aggregate/window results, and return selected expressions without row predicate methods.

```go
o := models.OrderFields()
total := o.TotalCents.Sum()
amount := query.CoalesceValue(
    total.Value(),
    total.Param(decimal.FromInt64(0)).Value(),
)
result, err := query.SelectValue(models.QueryOrders(), amount).RequireFirst(ctx, db)
```

`WhenValue` accepts `HavingPredicate` conditions. `Grouped(rowPredicate)` explicitly lifts a row condition; all referenced columns must still satisfy the surrounding query's grouping rules. Ordinary aggregates nested inside conditional values establish grouping, including when a window consumes those values. Nested windows remain invalid at one SELECT level, and wrapping a window in a conditional expression does not make it valid in HAVING.

Supported subqueries and CTEs remain discoverable inside conditions and selected values. Relation and correlation qualification traverse the same expression tree. No alternate raw SQL renderer is introduced.

## Bound parameters and codecs

`field.Param(v)` captures the field's non-nullable Go type and codec without adding a column reference. `RowExpression.Param(v)` and `Expression.Param(v)` use their exact result type; nullable ordered aggregates accept their concrete comparison type, as shown above. `NullFor` provides typed NULL parameters.

Encoding occurs once when the parameter is constructed, owning any returned byte buffer. Construction errors are retained and returned before execution. All parameters must be valid even if a CASE branch would not be selected. SQL CASE is also not a general shield against planning-time errors or earlier aggregate evaluation. [PostgreSQL evaluation rules](https://www.postgresql.org/docs/18/sql-expressions.html#SYNTAX-EXP-EVAL)

Parameters are bound, never interpolated. The compiler adds a cast from the codec's `ParameterType`, preventing an all-parameter CASE from becoming text and acquiring text ordering. [PostgreSQL type resolution](https://www.postgresql.org/docs/18/typeconv-union-case.html)

Built-in codecs declare boolean, bigint, double precision, text, UUID, numeric, date, time without time zone, timestamp without time zone or timestamp with time zone representations. Integer widths and unsigned bounds remain enforced by Go codecs; exact decimals retain numeric representation. Nullable and validated codecs preserve this metadata.

Custom codecs opt in with `WithParameterType`, including `TypeBytes` for bytea. They own compatibility between the declared representation and their encode/decode callbacks. Unspecified or unsupported parameter types fail compilation of a parameter expression. This metadata is not a column migration or physical-schema declaration; ordinary column-bound predicates retain their existing codec behavior.

Expression depth, node and parameter limits apply before execution. [Computed query keys](computed-query-keys.md) connect row and selected values to grouping, partitioning and DISTINCT ON. [Typed arithmetic and text calculations](scalar-calculations.md) extend this same expression layer; broader operations, including JSON, remain milestone 06 work.
