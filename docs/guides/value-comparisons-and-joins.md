# Typed value comparisons and join conditions

<!-- compiled-examples -->

Column predicates such as `orm.NumberGte(Users.Age, 18)` compare a typed literal. For two SQL
operands use `expr.Equal`, `NotEqual`, `Less`, `LessOrEqual`, `Greater` or
`GreaterOrEqual`. Both operands must retain the same scope and exact value type.
Ordered/text capability comes from the actual declaration, not just a Go underlying
type. Enums do not gain numeric or text operators.

## Row and selected comparisons

`expr.Like(text, pattern)` accepts typed text operands; wildcard characters retain
SQL meaning. Text `Contains` escapes wildcards for literal substring matching.
The `...Value` comparison forms accept selected values and produce
`expr.HavingPredicate[S]`; they cannot filter a row WHERE. Windows remain invalid
in HAVING even when nested inside a calculation.

## NULL semantics

`expr.NullableRow` explicitly widens a required operand. Nullable ordered/text
comparisons use their nullable families. Ordinary SQL comparisons with NULL
produce unknown; `DistinctFrom` and `NotDistinctFrom` provide NULL-aware equality.
These are predicates, separate from result DISTINCT clauses.

## Computed join conditions

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/comparisonqueries/joins_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/comparisonqueries/joins_postgres_test.go -->
```go
q := models.Users.Query()
younger, older := rowset.As[youngerAlias](q, "younger"), rowset.As[olderAlias](q, "older")
y, o := rowset.Scope(younger), rowset.Scope(older)
adjacent := rowset.OnEqual(expr.Add(expr.At(y, orm.NumberExpr(models.Users.Age)), expr.ParamOf(expr.AsSelected(expr.Row(expr.At(y, orm.NumberExpr(models.Users.Age)))), 1)), expr.At(o, orm.NumberExpr(models.Users.Age)))
```

The `rowset` import is `database/orm/rows`; alias tags are in the linked package.
`rows.OnEqual`, `OnNotEqual`, `OnLess`, `OnLessOrEqual`, `OnGreater`,
`OnGreaterOrEqual` and `OnLike` retain distinct side scopes and matching value
types. Nullable and NULL-aware families retain the same rules. Compose with
`OnAnd`, `OnOr`, `Not`, `WhereLeft` and `WhereRight`.

ON and final WHERE have different outer-join behavior. PostgreSQL still owns
operator/planner constraints: a pure inequality FULL JOIN can fail with SQLSTATE
`0A000`; adding a usable equality join key can make it supported.

## Cartesian joins

`rows.CrossJoin` combines independent aliased inputs. An empty input yields no
pairs; otherwise m and n input rows yield m × n pairs. Input windows stay inside
their SELECTs. Final ordering, counting, projections and locks use normal joined
scopes; earlier missing sides remain nullable.

## Scalar subqueries in row calculations

`rows.ScalarRowQuery` and `ScalarNullableRowQuery` retain an independent inner
SELECT. Correlated counterparts explicitly retain their outer owner. An absent
inner result yields NULL; multiple rows produce a cardinality error. No helper
adds an implicit limit. Inner aggregates/windows remain inside that subquery;
a direct aggregate cannot masquerade as a row operand.

See [derived records](model-subqueries.md), [correlations](model-correlations.md)
and [lateral joins](lateral-joins.md).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
