# Transaction-required query composition

<a id="agent-entry"></a>
## Agent entry

- [Compose a locked CTE](#recipe-locked-cte)
- [Lock a model](row-locking.md#recipe-lock-models)

## Overview

Use `database/orm/rows` transaction sources when a derived SELECT or CTE must
retain row locks. Its executable result requires `*database.Tx`; ordinary source
adapters cannot erase that requirement. Handwritten model and rows declarations
are shared with ordinary reads.

<a id="recipe-locked-cte"></a>
## Lock inside a CTE

The application declares its model and the result struct, and one empty tag type for the alias; `Summaries` is the result struct's handwritten `rows.Define` schema. `database`, `orm`, `rows` and `expr` are the framework's `database`, `database/orm`, `database/orm/rows` and `database/orm/expr` packages:

<!-- compiled-source: tests/fixtures/consumer/transactionqueries/models.go#transaction-models -->
```go
//foundry:model table=records var=Records key=ID identity=false
type Record struct {
	ID   int64 `foundry:"member=ID,key-role=lookup"`
	Name string
}

type Summary struct {
	ID   int64
	Name string
}
```

<!-- compiled-source: tests/fixtures/consumer/transactionqueries/models.go#transaction-aliases -->
```go
type claimedAlias struct{}
type otherAlias struct{}
```

`Claimed` locks and returns the first unlocked record inside the caller's transaction:

<!-- compiled-source: tests/fixtures/consumer/transactionqueries/models.go#transaction-query-composition -->
```go
func Claimed(ctx context.Context, tx *database.Tx) ([]Summary, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(orm.Lock(Records.Query().OrderBy(orm.Asc(Records.ID)).Limit(1)).SkipLocked()))
	source := rows.AsTransaction[claimedAlias](definition, "candidate")
	scope := rows.TransactionScope(source)
	return rows.SelectTransaction(source, Summaries,
		rows.MapSelected(Summaries.ID, expr.AsSelected(expr.Row(expr.At(scope, Records.ID)))),
		rows.MapSelected(Summaries.Name, expr.AsSelected(expr.Row(expr.At(scope, Records.Name))))).All(ctx, tx)
}
```

`rows.LockedRecord` adapts a locked model; `rows.TransactionCTE` retains
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
