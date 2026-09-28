# Transaction-required query composition

Use transaction composition when a CTE or derived query must acquire row locks for later work in the same transaction. Foundry retains each lock in the SELECT that declared it. Every executable result requires `*database.Tx`.

The [consumer fixture](../../tests/fixtures/consumer/transactionqueries/models.go) declares an ordinary model and a separate response projection:

```go
//foundry:model table=records primary=ID
type Record struct {
    ID   int64
    Name string
}

//foundry:projection
type Summary struct {
    ID   int64
    Name string
}
```

Generation supplies the same model fields and codecs used by ordinary queries. It also supplies transaction projection factories for declared projections.

## Lock inside a CTE

```go
type claimedAlias struct{}

definition := query.TransactionCTE("claimed",
    QueryRecords().OrderBy(RecordFields().ID.Asc()).Limit(1).
        ForUpdate().SkipLocked()).Materialized()
source := query.AsTransaction[claimedAlias](definition, "candidate")

records, err := query.SelectTransactionRecord(source, source.Scope()).All(ctx, tx)
```

`records` is `[]Record`. `First` returns `value.Optional[Record]`; `RequireFirst` returns `Record` or `database.NotFound`. `Each` streams complete records with the transaction cursor open. Collect a bounded slice before doing more work on that same transaction.

`TransactionCTE` accepts locked models, locked complete projections and other transaction record queries. Use `TransactionOf(ordinaryQuery)` to explicitly include an ordinary model/projection/CTE in a transaction-required composition. `AsTransaction` also accepts a locked query directly; it creates a derived SELECT with the original filter, ordering, limit and locks.

`Materialized` and `NotMaterialized` retain lock clauses. PostgreSQL may ignore an inlining request for a query with row locks. A lock on the outer SELECT does not lock CTE definitions; declare the lock within the CTE. See PostgreSQL's [locking clause](https://www.postgresql.org/docs/18/sql-select.html#SQL-FOR-UPDATE-SHARE).

## Generated projections and aggregates

```go
fields := RecordFieldsAt(source.Scope())
summaries, err := ProjectTransactionSummary(source).
    SelectID(fields.ID.Value()).
    SelectName(fields.Name.Value()).
    Query().All(ctx, tx)
```

The result is `[]Summary`; every selected field retains its input scope and exact value type. `SelectTransactionSummary(source, selection)` is the struct-selection alternative. Ordinary, correlated and transaction factories share the same projection declaration, field descriptors and decoder. A model does not automatically become a public response type.

Transaction projections provide `Where`, `OrderBy`, `GroupBy`, `Having`, `Limit`, `Offset`, `Count` and `Exists`. An outer aggregate may consume a locked CTE because the aggregate and the lock belong to different SELECT phases. `Count` counts the selected window while retaining inner locks. `Exists` only requests enough output to determine existence; it does not promise to lock every eligible row. The database may also push filters into a derived query. Express the intended inner filter/order/limit explicitly and keep the transaction open while acting on the returned records.

## Join transaction sources

```go
type otherAlias struct{}
other := query.AsTransaction[otherAlias](
    query.TransactionOf(QueryRecords()), "other")
a, b := RecordFieldsAt(source.Scope()), RecordFieldsAt(other.Scope())
joined := query.TransactionInnerJoin(source, other, query.On(a.ID, b.ID))
scope := query.LeftScope(joined, source.Scope())

records, err := query.SelectTransactionRecord(joined, scope).All(ctx, tx)
```

`TransactionLeftJoin`, `TransactionRightJoin`, `TransactionFullJoin` and `TransactionCrossJoin` share ordinary join conditions and scope helpers. Nullable sides require generated nullable fields and a declared projection. Complete record selection only accepts preserved scopes. Each source retains locks declared inside its own SELECT, including a source on the nullable side of an outer join; an outer lock cannot target that nullable side.

`ForUpdate`, `ForNoKeyUpdate`, `ForShare`, `ForKeyShare` and `LockRows` add locks to the outer SELECT. Use preserved `Of` scopes or per-input clauses to target its eligible underlying tables. These calls return the existing locked result type; use `TransactionCTE` or `AsTransaction` for further composition. An explicit outer CTE target, a grouped/window/distinct/set lock target or an invalid source fails before SQL.

## Scalar and predicate subqueries

`SelectTransactionValue(source, expression)` selects one exact value. Its `All`, `First`, `RequireFirst`, `Each`, `Count` and `Exists` terminals require a transaction. Applying a lock produces `LockedTransactionValue`, which retains the codec needed by scalar and membership subqueries. Lock policies remain immutable.

```go
inner := query.AsTransaction[otherAlias](
    query.TransactionOf(QueryRecords()), "inner_records")
a, b := RecordFieldsAt(source.Scope()), RecordFieldsAt(inner.Scope())
values := query.SelectTransactionValue(inner, b.ID.Value()).
    Where(b.ID.Eq(2)).ForUpdate()

records, err := query.SelectTransactionRecord(source, source.Scope()).
    Where(query.TransactionInQuery(query.Add(a.ID, a.ID.Param(1)), values)).
    All(ctx, tx)
```

`TransactionInQuery` accepts fields or computed row expressions with the exact inner result type. `TransactionInNullableQuery` explicitly admits NULL inner results. Negation retains SQL's three-valued logic. Selected aggregates/windows cannot be used as row operands.

`TransactionScalarQuery(source, values)` returns a nullable selected expression: no inner row becomes NULL, and multiple rows return PostgreSQL's cardinality error. No implicit limit hides that error. `TransactionScalarNullableQuery` keeps one nullable layer for an already nullable result. The corresponding `TransactionScalarRowQuery` and `TransactionScalarNullableRowQuery` helpers allow row predicates without moving the inner SELECT into the outer grouping/window phase.

`TransactionExistsQuery(source, inner)` accepts a transaction value/record source, including a locked model directly. `TransactionValueOf` and `TransactionSubqueryOf` explicitly lift ordinary sources into these contracts. Independent subqueries do not acquire outer-row locks implicitly. The [scalar acceptance](../../tests/fixtures/consumer/transactionqueries/subqueries_postgres_test.go) checks actual inner lock ownership, empty/one/multiple results, rollback after cardinality failures and nullable membership.

## Boundaries and failures

Transaction sources do not implement ordinary record, projection, join or value-source interfaces. They cannot be executed through a pool or session, or converted into an unrestricted query. The shared compiler additionally rejects nested locks through ordinary low-level generic query paths. `Compile` validates and returns a statement without executing it; manually executing its SQL is the explicit raw-SQL escape hatch and leaves the typed terminal contract.

Sources and query derivation are immutable. Invalid zero/nil sources fail validation; nil transactions and invalid contexts fail execution. SELECT depth, expression size and combined lock-validation work remain bounded. Model eager-loading clauses are not silently carried into SQL subqueries; select their persisted records explicitly instead.

Locks follow transaction/savepoint lifetime, including rollback, successful savepoint release and server-error behavior described in [row locking](row-locking.md#lifetime-failures-and-resource-bounds). The [PostgreSQL acceptance](../../tests/fixtures/consumer/transactionqueries/composition_postgres_test.go) checks these boundaries using independent transactions. Use [transaction-required correlation and lateral joins](transaction-correlations.md) when an inner locked query depends on a parent row; the scalar helpers above describe independent subqueries.
