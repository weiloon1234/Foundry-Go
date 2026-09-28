# Typed arithmetic and text calculations

Row calculations preserve their model or alias scope and compose with generated fields, [conditional values](conditional-expressions.md), predicates and ordering. Values and patterns are bound through their codecs; callers never supply SQL function or operator names.

```go
f := models.UserFields()
nextAge := query.Add(f.Age, f.Age.Param(1))
email := query.Lower(query.Trim(f.Email))

users, err := models.QueryUsers().
    Where(nextAge.Gte(18), email.Contains("@example.com")).
    OrderBy(email.Asc(), f.ID.Asc()).
    All(ctx, db)
```

`users` remains `[]models.User`. These calculations run in PostgreSQL before hydration. For in-memory slice operations, see [model queries](model-queries.md).

## Numeric values

`Add`, `Subtract`, `Multiply`, `Divide`, `Remainder`, `Negate` and `Abs` preserve the concrete numeric Go type and codec. Binary operations require the same scope and value type on both sides. `Remainder` accepts integers and exact decimals, excluding floats. Generated nominal numeric types retain their validation codec.

Arithmetic uses canonical SQL bigint, numeric or double precision operands. Physical smallint and real columns can participate without changing their declared Go result type. SQL overflow and division by zero return database errors; a result outside a narrow integer, unsigned or validated domain fails decoding. A collected read returns no partial slice on failure.

Integer division truncates toward zero. For fractional division of integer inputs, convert explicitly with `DecimalOf` before `Divide`. `DecimalOf` accepts exact integers/decimals and produces `decimal.Decimal`; floating inputs are excluded. `FloatOf` deliberately produces float64 and can lose precision. Decimal division follows PostgreSQL numeric precision, not an arbitrary rational-number contract. See [PostgreSQL arithmetic](https://www.postgresql.org/docs/18/functions-math.html).

`AddNullable` and corresponding nullable operations retain `value.Nullable[V]`. Both operands must be nullable; use `NullableRow(nonNullValue)` to widen one explicitly. SQL NULL propagates through these operations. Ordered nullable expressions take concrete comparison values: `total.Gt(amount)` tests a non-null amount, while `total.IsNull()` tests absence. `DecimalNullable` and `FloatNullable` preserve this distinction during conversion.

## Text values

Text functions accept ordinary or named string inputs. Transformations return plain `string`, since transformed text is not automatically a valid value of the original named domain or enum.

| Row function | Result or behavior |
| --- | --- |
| `Lower`, `Upper` | Database case conversion |
| `Trim`, `TrimLeft`, `TrimRight` | Remove ordinary spaces at the indicated ends |
| `TrimChars`, `TrimLeftChars`, `TrimRightChars` | Remove characters supplied by a typed text operand |
| `Concat` | Concatenate two text operands |
| `ConcatWS` | Join text operands with a bound separator |
| `Replace` | Replace occurrences using typed search and replacement operands |
| `Length`, `OctetLength` | Character count or byte count, as int64 |
| `Substring`, `SubstringFrom` | Extract using bound int32 positions/counts |
| `SubstringAt` | Extract using typed integer expressions for start and count |

Case conversion follows the database's locale/collation. Character counts are not grapheme-cluster counts, and ordinary-space trimming is not Go's Unicode `strings.TrimSpace`. Substring positions follow SQL's one-based convention. See [PostgreSQL string functions](https://www.postgresql.org/docs/18/functions-string.html).

`Lower(...).Like("%admin%")` accepts a SQL LIKE pattern. `Lower(...).Contains("_20%")` escapes wildcard characters and searches for that literal substring. Pattern values remain bound. Text comparisons retain the original query scope.

Nullable variants such as `LowerNullable`, `ConcatNullable` and `SubstringNullable` propagate NULL. `ConcatWSNullable` instead skips NULL operands, preserves empty strings and returns non-null text; an all-NULL input produces an empty string. Its separator is a non-null Go string, and its variadic operands share a concrete string type. Widen non-null operands with `NullableRow`, or normalize differently named strings with a text transformation first. The compiler uses a typed variadic array to avoid PostgreSQL's ordinary function-argument count limit; framework expression bounds still apply.

Negative literal substring counts fail query compilation before executor access, including for a zero-row limit. `SubstringAt` accepts integer expressions, so negative counts and values outside SQL integer bounds are database errors. Construction-time validation cannot know arbitrary row values.

## Selected values and comparisons

The `...Value` counterparts compose selected expressions, including ordinary aggregates and window outputs. For example, `MultiplyValue(count.Value(), count.Param(2).Value())` calculates a scaled count. Nullable counterparts retain nullable results. These APIs do not convert selected expressions into row predicates.

`CompareValue(expr)` exposes equality/membership and NULL checks for HAVING. `OrderValue(expr)` adds ordered comparisons; `OrderNullableValue(expr)` accepts concrete non-null comparison values. `TextValue` and `TextNullableValue` add LIKE and literal substring comparisons. SQL grouping restrictions still apply, and wrapping a window expression does not make it legal in HAVING or inside another window.

Row calculations expose `Value()` for declared projections, `Group()` for computed grouping and `Asc()`/`Desc()` for ordering. Their AST participates in dependency discovery, explicit correlations, [computed keys](computed-query-keys.md) and named-window discovery. To aggregate a computed projection field, declare the projection and use its generated fields from a derived source, as shown by the [consumer composition tests](../../tests/fixtures/consumer/scalarqueries/composition_postgres_test.go).

## Computed model ordering

Generated model `OrderBy` accepts row calculations directly. Numbered/simple model pages and offset chunks append the actual primary key when required for a unique tie-breaker. Computed orders also work in transaction-scoped model reads and direct or many-to-many relation scopes, including pivot ordering.

Model cursor and primary-key chunk APIs do not infer cursor codecs or identities from arbitrary calculations. Computed model cursor orders fail before execution. For computed result cursors, select a declared projection and use [result cursor pagination](result-cursor-pagination.md) with explicit output fields and uniqueness. Sorting an already loaded slice remains a separate in-memory operation.

These operations extend the shared expression AST. [Value comparisons and joins](value-comparisons-and-joins.md) compare computed operands in row/selected conditions and join clauses, including scalar subqueries with explicit phase boundaries. Typed temporal/JSON operations and conflict calculations remain milestone 06 work. The [master blueprint](../../blueprint/00-master-architecture-and-parity.md) records verification and remaining scope.
