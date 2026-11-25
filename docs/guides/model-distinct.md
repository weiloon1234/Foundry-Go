# Typed distinct reads

DISTINCT removes duplicate selected rows; DISTINCT ON chooses one ordered row per
key combination. These are read-only selections that preserve the concrete result
type, source boundary and codecs.

## Unique values and complete models

Use `rows.FromModel` followed by `rows.Distinct` for a complete-model projection,
`rows.Distinct` for a rows projection, or `rows.DistinctRecord` for a complete
record selected from a join. `models.User` and `models.Order` are the application's models, an order holding its buyer in `BuyerID`; `rows` and `expr` are the framework's `database/orm/rows` and `database/orm/expr` packages. Two empty tag types alias the joined sources:

<!-- compiled-source: tests/fixtures/consumer/distinctqueries/guide_distinct.go#distinct-aliases -->
```go
type PersonAlias struct{}
type PurchaseAlias struct{}
```

The block joins users to their orders, selects the ordered user records and removes duplicates:

<!-- compiled-source: tests/fixtures/consumer/distinctqueries/guide_distinct.go#model-distinct -->
```go
people := rows.As[PersonAlias](models.Users.Query(), "person")
orders := rows.As[PurchaseAlias](models.Orders.Query(), "purchase")
p, o := rows.Scope(people), rows.Scope(orders)
joined := rows.InnerJoin(people, orders, rows.On(expr.At(p, models.Users.ID), expr.At(o, models.Orders.BuyerID)))
scope := rows.LeftScope(joined, rows.Scope(people))
base := rows.SelectRecord(joined, scope).OrderBy(expr.Asc(expr.At(scope, models.Users.Email)))
unique := rows.DistinctRecord(base)
```

Read the result with `rows.ReadRecord(unique).All(ctx, db)`. Distinctness covers
all selected columns, including a complete model's primary key. Use a smaller
rows schema or scalar selection to deduplicate fewer values. SQL NULL values
compare as duplicates without changing hydration nullability. Eager-loading
clauses are rejected on these read-only sources.

## One ordered row per group

`rows.DistinctOn(q, expr.Group(row), ...)` accepts row keys;
`rows.DistinctOnValues(q, expr.Key(selected), ...)` accepts selected keys. These keys
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
