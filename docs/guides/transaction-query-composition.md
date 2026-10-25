# Transaction-required query composition

<!-- compiled-examples -->

Use `database/orm/rows` transaction sources when a derived SELECT or CTE must
retain row locks. Its executable result requires `*database.Tx`; ordinary source
adapters cannot erase that requirement. Handwritten model and rows declarations
are shared with ordinary reads.

## Lock inside a CTE

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/transactionqueries/models.go):

<!-- go-source: tests/fixtures/consumer/transactionqueries/models.go -->
```go
func Claimed(ctx context.Context, tx *database.Tx) ([]Summary, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(Records.Query().OrderBy(Records.ID.Asc()).Limit(1).ForUpdate().SkipLocked()))
	source := rows.AsTransaction[claimedAlias](definition, "candidate")
	fields := recordAt(rows.TransactionScope(source))
	return rows.SelectTransaction(source, Summaries, rows.MapSelected(Summaries.ID, expr.Select(fields.ID)), rows.MapSelected(Summaries.Name, expr.Select(fields.Name))).All(ctx, tx)
}
```

The linked package owns `Records`, `Summaries`, alias tags and the `recordAt`
helper. `rows.LockedRecord` adapts a locked model; `rows.TransactionCTE` retains
its inner lock. `rows.TransactionOf(rows.ModelRecord(query))` explicitly admits an
ordinary record into transaction-required composition. `AsTransaction` accepts
transaction sources; `TransactionScope` exposes their typed fields.

Materialization requests do not remove locks. PostgreSQL may ignore an inlining
request for a locked definition. An outer lock does not lock the CTE definition;
put the lock on the SELECT that should acquire it.

## Handwritten projections and aggregates

`rows.SelectTransaction(source, schema, mappings...)` selects a DTO;
`SelectTransactionRecord` selects a preserved complete record. Both keep exact
value/nullability types and require the caller's transaction. An outer aggregate
may consume a locked CTE because the lock and aggregate occupy different SELECTs.
Count observes the result window; Exists need not read or lock all candidates.

## Join transaction sources

`TransactionInnerJoin`, `TransactionLeftJoin`, `TransactionRightJoin`,
`TransactionFullJoin` and `TransactionCrossJoin` use typed join conditions and
ordinary preserved/nullable scope helpers. Each input retains locks declared in
its own SELECT. A missing outer-join side requires nullable mappings; an outer
lock cannot target it.

## Subqueries and failures

Transaction scalar/membership/existence adapters preserve the same NULL and
cardinality rules as ordinary subqueries. `TransactionValueOf` and
`TransactionSubqueryOf` explicitly admit ordinary inner sources. Correlated locks
use the [transaction correlation family](transaction-correlations.md).

Compile/preflight validate before executor access. Execute sequentially on the
supplied transaction; a streaming cursor owns its connection until closed.
Collect a bounded result before other work on that transaction. Database lock,
cardinality and cancellation failures follow its transaction/savepoint rules.
`SkipLocked` deliberately permits partial candidate sets and is unsuitable for
operations that require a complete bounded set.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
