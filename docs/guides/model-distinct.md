# Typed distinct reads

<!-- compiled-examples -->

DISTINCT removes duplicate selected rows; DISTINCT ON chooses one ordered row per
key combination. These are read-only selections that preserve the concrete result
type, source boundary and codecs.

## Unique values and complete models

Use `rows.FromModel` followed by `rows.Distinct` for a complete-model projection,
`rows.Distinct` for a rows projection, or `rows.DistinctRecord` for a complete
record selected from a join:

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/distinctqueries/distinct_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/distinctqueries/distinct_postgres_test.go -->
```go
people := rows.As[personAlias](models.Users.Query(), "person")
orders := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
p, o := rows.Scope(people), rows.Scope(orders)
joined := rows.InnerJoin(people, orders, rows.On(expr.At(p, orm.KeyExpr(models.Users.ID)), expr.At(o, orm.ColumnExpr(models.Orders.BuyerID))))
scope := rows.LeftScope(joined, rows.Scope(people))
fields := scope
base := rows.SelectRecord(joined, scope).OrderBy(expr.At(fields, orm.TextExpr(models.Users.Email)).Asc())
unique := rows.DistinctRecord(base)
items, err := rows.ReadRecord(unique).All(t.Context(), tx)
```

The linked package declares `personAlias` and `purchaseAlias`. Distinctness covers
all selected columns, including a complete model's primary key. Use a smaller
rows schema or scalar selection to deduplicate fewer values. SQL NULL values
compare as duplicates without changing hydration nullability. Eager-loading
clauses are rejected on these read-only sources.

## One ordered row per group

`rows.DistinctOn(q, expr.Group(row), ...)` accepts row keys;
`rows.DistinctOnValues(q, selected.Key(), ...)` accepts selected keys. These keys
need not be output fields. Start ordering with the distinct keys, then desired
precedence and a unique tie-breaker. The compiler accepts a permutation or shortened
prefix of distinct keys, but an unrelated order can only follow all distinct keys.
A valid SQL order alone does not guarantee a deterministic winner among ties.

Keys must be nonzero, unique and owned by the input. Selected aggregate/window
keys retain their phase; grouped SELECTs still require grouped ordinary references.
See [computed keys](computed-query-keys.md).

| Read family | Clause functions in `rows` |
| --- | --- |
| Projection / `FromModel` | `Distinct`, `DistinctOn`, `DistinctOnValues` |
| Scalar | `DistinctScalar`, `DistinctOnScalar`, `DistinctOnScalarValues` |
| Complete record | `DistinctRecord`, `DistinctOnRecord` (input-scoped ordering) |
| Record set | `DistinctSet` |
| Value set | `DistinctValues`, `DistinctOnValueSet` (selected keys) |
| Correlated record | `DistinctCorrelatedRecord` |
| Correlated scalar | `DistinctOnCorrelatedScalar`, `DistinctOnCorrelatedScalarValues` |

## Ordering, counting and composition

Ordinary DISTINCT requires order expressions to match selected expressions,
including their encoded parameter values. Matching outputs reuse positions and
bindings. To order by another output criterion, create a derived alias first.
DISTINCT ON and DISTINCT replace one another on an immutable builder.

Deduplication follows WHERE and grouping/HAVING, preceding limit/offset. To
deduplicate an already-limited input, wrap that input in a derived source. Count
observes the complete selected window. First/Exists retain offset/distinctness
and respect `Limit(0)`. The [common read terminals](model-projections.md#execution-and-failures)
keep normal collection, streaming and cancellation behavior. Read-only first-row
selection adds no implicit ordering. SQL equality support and collation behavior
remain database constraints; a typed declaration cannot prove them.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
