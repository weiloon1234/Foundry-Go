# Transaction-required correlation and lateral joins

<!-- compiled-examples -->

Use these APIs when an inner query depends on a parent row and must retain locks.
Both outer ownership and the transaction requirement survive composition. The
only executable result belongs to the enclosing transaction query.

## Declare the parent and child

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/transactionqueries/correlations_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/transactionqueries/correlations_postgres_test.go -->
```go
parent := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.KeyIn(Records.ID, 1, 3)))), "parent")
child := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "child")
c := rows.TransactionCorrelate(parent, child)
p, f := recordAt(rows.TransactionOuterScope(c, rows.TransactionScope(parent))), recordAt(rows.TransactionInnerScope(c, rows.TransactionScope(child)))
c = c.Where(expr.Greater(f.ID, p.ID))
```

The linked package declares the aliases and `recordAt` helper. Lift inputs through
`TransactionOuterScope`/`TransactionInnerScope`; nullable equivalents preserve
missing joined rows. A nested transaction correlation can explicitly capture its
parent and grandparent. Plain predicates from an unlifted source are not substitutes
for the combined scope.

## Select a locked correlated result

`rows.ProjectTransactionCorrelated(c, schema, mappings...)` creates a complete
DTO result. `SelectTransactionCorrelatedRecord` selects a preserved inner model.
`AsTransactionLateral` followed by `TransactionLeftJoinLateral` retains unmatched
parents; inner/cross variants keep matching records. Use nullable rows/column
helpers for missing right fields.

The correlated record owns its order, per-parent limit and lock clauses. Ordinary
aliases, joins and CTEs cannot consume it. ON conditions and final WHERE remain
separate from filtering before that per-parent limit.

## Correlated scalars and membership

`SelectTransactionCorrelatedScalar` retains the outer type and exact selected
value. `rows.TransactionCorrelatedScalarRowQuery(selected).Value()` yields a
nullable outer selected value; the nullable row constructor keeps one layer.
Use the row expression directly for outer predicates. An empty inner result becomes NULL; multiple rows remain a cardinality error.

`TransactionInCorrelatedQuery`, `TransactionInNullableCorrelatedQuery` and
`Exists()` retain outer ownership and SQL's three-valued NULL behavior.

## Lock policies and failures

Use `ForUpdate`, `ForNoKeyUpdate`, `ForShare`, `ForKeyShare`, `Of`, `LockRows`,
`NoWait`, `SkipLocked` and `Wait` on supported transaction selections. `Of` takes
`rows.Target(scope)`. Policies need an existing lock; grouped/distinct/window/set
lock targets and an outer parent used as an inner lock target fail preflight.

Locks stay in the SELECT that declared them. The number of returned rows does
not promise the number of acquired locks. Concrete aliases, scopes, bounds and
source capabilities are validated before I/O. Lock contention, cancellation and
cardinality failures retain normal transaction/savepoint recovery requirements.
See [transaction composition](transaction-query-composition.md) and
[row locking](row-locking.md).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
