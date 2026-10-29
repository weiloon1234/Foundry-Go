# Transaction-scoped row locking

Use a locked read when domain work must inspect a row and then change it while excluding conflicting writers. Every locked terminal requires `*database.Tx`. Foundry does not open a short transaction around only the read: the caller owns the transaction containing the protected operation.

```go
err := db.Transaction(ctx, func(tx *database.Tx) error {
    user, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Users.Query().Where(orm.KeyEq(models.Users.ID, userID))))
    if err != nil {
        return err
    }
    _, err = orm.Update(ctx, tx, models.Users.Query(), models.Users.Key(), user.ID, orm.NumberSet(models.Users.Age, user.Age + 1))
    return err
})
```

`orm.Find` and `orm.RequireKey` retain the model's exact primary-key type, including natural keys. `orm.FirstLocked` returns `value.Optional[models.User]`, `orm.RequireLocked` returns a model or `database.NotFound`, and `orm.AllLocked` returns `[]models.User`. Model `First` defaults to primary-key order, honors filters/offsets, and preserves a zero limit. Declared projections and single-value selections also support locking, retaining their exact result types.

## Strength and waiting

- `orm.Lock(query)` requests an exclusive row lock.
- `orm.ForNoKeyUpdate(query)` allows concurrent `FOR KEY SHARE` reads.
- `orm.ForShare(query)` allows compatible shared locks while preventing row updates/deletion.
- `orm.ForKeyShare(query)` prevents deletion and updates that change the relevant key.

Waiting is the default. `NoWait()` returns the PostgreSQL lock error when a row cannot be locked immediately. `SkipLocked()` omits conflicting rows, which is useful for competing queue consumers. `Wait()` restores normal waiting; the last waiting modifier wins. These policies affect row locks, not every table lock required by PostgreSQL. Use a bounded context for the entire transaction and shorter operation contexts where appropriate.

```go
claimed, err := orm.AllLocked(ctx, tx, orm.Lock(models.Users.Query().
    Where(orm.EnumEq(models.Users.Status, models.StatusActive)).
    OrderBy(orm.KeyAsc(models.Users.ID)).Limit(20)).SkipLocked())
```

An empty `SkipLocked` result does not prove that no eligible rows exist. Locks protect existing rows; a missing `First` does not reserve an absent key. Use unique constraints and [typed upserts](model-upserts.md) for insert conflicts.

## Typed joins and projections

Locking a join normally locks contributing rows from all eligible input tables. Use `Of` with preserved source scopes to select which tables to lock. The nullable side of an outer join cannot be row-locked; its nullable scope cannot produce a `rows.LockTarget`. An unqualified lock on such a join is rejected before execution.

```go
type userAlias struct{}
type orderAlias struct{}
users := rows.As[userAlias](models.Users.Query(), "u")
orders := rows.As[orderAlias](models.Orders.Query(), "o")
joined := rows.LeftJoin(users, orders,
    rows.On(expr.At(rows.Scope(users), orm.KeyExpr(models.Users.ID)),
        expr.At(rows.Scope(orders), orm.ColumnExpr(models.Orders.BuyerID))))
userScope := rows.LeftScope(joined, rows.Scope(users))
lockedUsers, err := rows.ForUpdate(rows.ReadRecord(rows.SelectRecord(joined, userScope))).
    Of(rows.Target(userScope)).All(ctx, tx)
```

`Of` replaces its previous target list. Targets must be nonempty, unique and present in the query's input scope. Self-joins use typed aliases and their remapped join scopes. Complete record selection does not deduplicate repeated join rows.

Ordinary derived sources propagate a lock to their underlying tables. Aggregate, grouped, distinct, window and set-operation results cannot be lock targets. Predicate and scalar subqueries are independent reads. Outer locking does not propagate into a CTE; explicit `Of` of a CTE and queries with no lockable underlying table are rejected. Locked builders cannot be converted into unrestricted sources, count queries, paginated queries or mutations. Use [transaction-required composition](transaction-query-composition.md) to retain a locked record inside a CTE, derived table, join or declared projection. Its terminals still require the caller's transaction.

## Different policies for joined tables

Use `LockRows` on a complete record, projection or single-value selection to declare each table's lock policy. Both scopes must belong to the same query and be non-nullable:

```go
joined := rows.InnerJoin(users, orders,
    rows.On(expr.At(rows.Scope(users), orm.KeyExpr(models.Users.ID)),
        expr.At(rows.Scope(orders), orm.ColumnExpr(models.Orders.BuyerID))))
userScope := rows.LeftScope(joined, rows.Scope(users))
orderScope := rows.RightScope(joined, rows.Scope(orders))
locked := rows.LockRows(rows.ReadRecord(rows.SelectRecord(joined, userScope)),
    rows.UpdateLock(rows.Target(userScope)).NoWait(),
    rows.ShareLock(rows.Target(orderScope)).SkipLocked())
```

`UpdateLock`, `NoKeyUpdateLock`, `ShareLock` and `KeyShareLock` accept one or more preserved input scopes. Each returns an immutable `RowLock[Scope]` descriptor with `NoWait`, `SkipLocked` and `Wait` modifiers. A modifier on the resulting locked query changes every clause; the original query stays unchanged. `Of` replaces targets only when the query has a single clause. For multiple clauses, supply each target list in its descriptor.

Targets within one clause must be unique. Separate clauses may overlap: PostgreSQL uses the strongest lock affecting a table, with `NoWait` taking precedence over `SkipLocked`, and `SkipLocked` over waiting, regardless of clause order. A policy on a different table remains independent. Foundry validates every clause and bounds their combined validation work before execution. The [consumer acceptance](../../tests/fixtures/consumer/lockqueries/clauses_postgres_test.go) exercises these policies with competing transactions.

## Lifetime, failures and resource bounds

Locks belong to the transaction; closing a result cursor is not an unlock operation. Rolling back a nested transaction releases locks acquired in that savepoint; releasing a successful savepoint retains its locks until the outer transaction ends. Client-side decoder/callback failures return without requesting transaction rollback. Return the error from the transaction callback, or isolate a recoverable attempt in a nested transaction. Server errors, context cancellation and connection failure can abort/end the transaction and its locks; do not assume protection survives them. PostgreSQL errors such as `NOWAIT` contention require rollback before that scope can execute more SQL. Foundry preserves `database.Error.SQLState()` and does not automatically retry the protected operation.

`With(...)` loads related models on the same transaction after closing the parent rows. The lock applies to parents; the related queries do not inherit it. Locked `orm.EachLocked` requires no eager-loading clauses, and its callback runs with the row cursor open. The same transaction cannot execute another operation during that callback; use a bounded `All` collection before processing writes. On callback failure, rows close; a driver may drain unread rows, so callback termination is not an exact bound on acquired locks. Use `Limit` to bound selection. Partial results are discarded on hydration/cleanup/relation failures.

`OFFSET` can lock skipped rows. `SKIP LOCKED` changes the returned set, and concurrent updates under `READ COMMITTED` can affect order while a query waits. These are database semantics, not promises of snapshot consistency. See PostgreSQL's [locking clause](https://www.postgresql.org/docs/18/sql-select.html#SQL-FOR-UPDATE-SHARE) and [row-level lock modes](https://www.postgresql.org/docs/18/explicit-locking.html#LOCKING-ROWS).
