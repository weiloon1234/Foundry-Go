# Computed query keys

Typed row expressions can supply `GROUP BY`, `DISTINCT ON` and window partition keys. They share the query AST, codecs and parameter binding used by [conditional expressions](conditional-expressions.md). The [independent consumer](../../tests/fixtures/consumer/keyqueries/) demonstrates grouping, complete model selection, aggregate/window keys, CTEs, correlations and failure boundaries.

## Group by a computed value

```go
u := models.UserFields()
bucket := query.When(u.Age.Gt(20), u.Age.Param(1)).Else(u.Age.Param(0))

counts, err := query.SelectValue(
    models.QueryUsers(), query.Count[models.User]().Value(),
).GroupBy(bucket.Group()).OrderBy(bucket.Asc()).All(ctx, db)
```

The result is `[]int64`. To return each bucket beside its count, declare a projection with `Bucket int` and `Count int64`, as in the [consumer report](../../tests/fixtures/consumer/keyqueries/reports.go). The projection is a complete declared result, separate from a persisted model.

`RowExpression.Group()` preserves its scope and erases only its value type, allowing mixed key types in `...Group[Scope]`. Existing `field.Group()` calls and slices of group descriptors remain valid. `GroupBy` appends keys. Repeated keys and zero descriptors fail query compilation.

A computed grouping key is one grouped value. Grouping by the bucket does not make `u.Age` independently selectable. Selecting the bucket, using it inside a larger selected expression, or filtering with `Having(query.Grouped(bucket.Eq(1)))` is supported. Aggregate expressions cannot become row grouping keys. These rules follow PostgreSQL's [grouping semantics](https://www.postgresql.org/docs/18/sql-select.html#SQL-GROUPBY).

## Select one model per computed key

```go
users, err := models.QueryUsers().
    DistinctOn(bucket.Group()).
    OrderBy(bucket.Asc(), u.Age.Desc(), u.ID.Asc()).
    All(ctx, db)
```

The result remains `[]models.User`, with one complete model per bucket. Order by the distinct keys first, then the desired precedence and a unique tie-breaker. [Distinct reads](model-distinct.md) describes prefix permutations, missing ordering, counting and pagination. No raw SQL or column strings are required.

## Aggregate and window keys

Selected values use `Expression.Key()` to produce `ProjectionKey[Scope]`. Pass these to `PartitionByValues` or `DistinctOnValues`; `GroupBy` accepts only row groups.

```go
count := query.Count[models.User]()
window := query.WindowFor(models.QueryUsers()).
    PartitionByValues(count.Value().Key())

positions, err := query.SelectValue(models.QueryUsers(), query.RowNumber(window)).
    GroupBy(bucket.Group()).OrderBy(count.Asc()).All(ctx, db)
```

Here the window partitions the already-grouped rows by their counts. Ordinary aggregates in selected keys establish grouping even when they are absent from the result list. Window functions cannot nest in another window partition at the same SELECT level; the query compiler rejects this before execution. See PostgreSQL's [window evaluation rules](https://www.postgresql.org/docs/18/sql-expressions.html#SYNTAX-WINDOW-FUNCTIONS).

`Window.PartitionBy(...Group)` accepts fields and computed row keys. `PartitionByValues(...ProjectionKey)` additionally accepts selected aggregate/scalar values. Both append keys to the same immutable window definition. Duplicate partition keys are rejected across both methods.

`DistinctOnValues` supports selected aggregate and window keys, following the same grouping and ordering requirements. It replaces an earlier distinct clause and retains the result type. Model, projection, scalar, combined and correlated queries use the same implementation; correlated results retain their outer ownership.

## Identity and scope boundaries

Equivalent keys match by their canonical compiled SQL and encoded parameter values. The compiler reuses the matched key's parameter positions within one SELECT, including grouped subexpressions inside a larger key. Different parameter values form different keys. Matching does not infer algebraic equivalence or database collation equivalence. Custom codecs must provide stable encodings; comparison and expression work have bounded compiler budgets.

Typed parameters are SQL values, including when they contain an integer. They do not become ordinal references to selected columns. NULL remains distinct from zero and empty values through codecs and hydration; SQL grouping puts NULL keys together.

CTE discovery and explicit correlation traverse key expressions. Grouping an outer computed value does not authorize a nested query to capture its ungrouped input columns. Expose the grouped value through a declared projection and alias/CTE when another query needs to address it as a field. Compiler grouping checks remain strict and do not infer functional dependencies from primary keys.

[Result cursor pagination](result-cursor-pagination.md) requires declared output fields for `UniqueBy`. First project a computed key, then use the generated output field in the cursor scope. A computed `Group()` supplied directly as a cursor identity is rejected.
