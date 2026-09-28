# Typed value comparisons and join conditions

Use generated field methods such as `fields.Age.Gte(18)` for literal conditions. When both operands are SQL expressions, use typed comparison functions. Values, scopes and nullability remain compiler-checked, and the same binary comparison node serves ordinary column comparisons, computed values and joins.

```go
f := models.UserFields()
nextAge := query.Add(f.Age, f.Age.Param(1))
users, err := models.QueryUsers().
    Where(query.GreaterOrEqual(nextAge, f.Age.Param(21))).
    OrderBy(f.Age.Asc()).
    All(ctx, db)
```

## Row and selected comparisons

`Equal`, `NotEqual`, `Less`, `LessOrEqual`, `Greater` and `GreaterOrEqual` compare row operands from the same query scope and with the same concrete Go value type. Ordered comparisons accept numeric, string and supported temporal values. Explicit [numeric conversions](scalar-calculations.md) are available when operand types differ. A model-specific ID cannot be compared to an unrelated model's ID.

`Like(text, pattern)` accepts two typed text operands. The pattern can be a field, calculation or bound parameter. Its wildcard characters retain their SQL meaning. For a literal substring, continue using `field.Contains(text)` or a computed text expression's `Contains` method.

The `...Value` counterparts accept selected `Expression` values and return `HavingPredicate` values. This allows comparisons between ordinary aggregates or aggregate calculations. They cannot be passed to row `Where` methods. SQL grouping rules still apply to both operands, and window expressions remain invalid in HAVING even when hidden inside calculations.

```go
count := query.Count[models.User]()
counts, err := query.SelectValue(models.QueryUsers(), count.Value()).
    Having(query.GreaterValue(count.Value(), count.Param(2).Value())).
    All(ctx, db)
```

## NULL semantics

Equality functions retain the exact operand type, including `value.Nullable[V]`. Use `NullableRow` to widen a non-null operand explicitly. Nullable ordered/text operands use `LessNullable`, `GreaterOrEqualNullable`, `LikeNullable` and corresponding `...Value` variants.

Ordinary SQL comparisons involving NULL yield unknown, so they do not satisfy WHERE or ON. `NotDistinctFrom` treats two NULL values as equal; `DistinctFrom` treats NULL as different from a non-null value. Selected variants retain the same behavior. These are SQL comparison predicates, separate from result deduplication through `Distinct` or `DistinctOn`. See [PostgreSQL comparison semantics](https://www.postgresql.org/docs/18/functions-comparison.html).

## Computed join conditions

The existing `On(leftKey, rightKey)` remains the compact equality join for compatible generated keys, including nullable keys. Computed joins use `OnEqual`, `OnNotEqual`, `OnLess`, `OnLessOrEqual`, `OnGreater`, `OnGreaterOrEqual` or `OnLike`. The operands retain different left/right input scopes but must agree on their concrete value type.

```go
type youngerAlias struct{}
type olderAlias struct{}

younger := query.As[youngerAlias](models.QueryUsers(), "younger")
older := query.As[olderAlias](models.QueryUsers(), "older")
y := models.UserFieldsAt(younger.Scope())
o := models.UserFieldsAt(older.Scope())

on := query.OnEqual(query.Add(y.Age, y.Age.Param(1)), o.Age)
joined := query.LeftJoin(younger, older, on)
```

`OnAnd`, `OnOr`, `Not`, `WhereLeft` and `WhereRight` compose these conditions without erasing their side ownership. Nullable ordered/text variants require nullable operands; `OnNotDistinctFrom` and `OnDistinctFrom` supply explicit NULL-aware comparisons. Computations never require a raw SQL operator or function name.

Select results through [typed joined scopes and declared projections](model-joins.md). Left/right/full joins preserve unmatched rows and expose nullable fields on the missing side. A predicate placed in ON can have different outer-row behavior from a predicate applied afterward with projection `Where`; Foundry preserves that distinction.

PostgreSQL requires a hash/merge-capable join key for FULL JOIN. A pure inequality condition is unsupported and returns database SQLSTATE `0A000`. An equality key combined with additional range conditions is supported; the consumer tests exercise both outcomes. Foundry preserves the requested condition rather than replacing it with a different join. Operator classes and planner eligibility remain database responsibilities. See the [PostgreSQL 18 planner source](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/optimizer/path/joinrels.c).

## Cartesian joins

`CrossJoin(left, right)` combines independent aliased sources without an ON condition. Its result has a distinct `Cross[L, R]` scope. Use `LeftScope` and `RightScope` for preserved model sides, and `LeftNullableScope` when the preceding join chain already contains a nullable side.

Input filters, ordering and limits remain inside their original source boundaries. Two non-empty inputs with m and n rows produce m × n pairs; an empty input produces no pairs. Joined results keep duplicates and do not infer uniqueness from one model's ID. Add final ordering when result order matters. The normal result runtime supports declared projections, complete-record selection, counting, pagination and transaction-scoped locks. See [PostgreSQL table expressions](https://www.postgresql.org/docs/18/queries-table-expressions.html#QUERIES-JOIN).

## Scalar subqueries in row calculations

`ScalarRowQuery(outer, inner)` supplies a scalar subquery as a `RowExpression` in the outer scope. `ScalarNullableRowQuery` preserves one nullable layer for an already-nullable inner value. The outer argument supplies the scope; its filters are not copied implicitly.

`CorrelatedScalarRowQuery` and `CorrelatedScalarNullableRowQuery` use [explicit correlation descriptors](model-correlations.md). Ordinary uncorrelated subqueries cannot capture outer fields accidentally. Related-model filters reuse framework alias qualification, including nested scalar operands; consumers do not supply loader aliases.

Each inner SELECT retains its own aggregate/window phase. It may produce one scalar row, no row (SQL NULL), or an invalid multi-row result (a database cardinality error). Foundry adds no implicit limit. Direct aggregate/window expressions still cannot masquerade as row values, and aggregates over only an outer column are rejected because they would change SQL query ownership.

The [consumer comparison fixtures](../../tests/fixtures/consumer/comparisonqueries) demonstrate row/selected comparisons, outer and Cartesian joins, scalar cardinality, CTE discovery and relation aliasing. [Milestone 06](../../blueprint/06-relations-and-advanced-queries.md) still includes typed temporal/JSON operations, conflict expressions/targets and transaction-preserving composed locks. The master records verification evidence.

[Lateral joins](lateral-joins.md) extend the same join and correlation machinery to right-hand queries that depend on preceding rows.
