# Complete records and transaction projections

`ModelRecord(query)` adapts a complete model query. `RecordOf(projection)` adapts
an existing `rows.Select` or `rows.FromModel` result. The adapter retains ordered
columns, the declaration identity and decoder, filters, ordering and windows.
It has no execution or mutation methods. Scalar-only queries and zero adapters
fail validation before SQL; `SelectScalar` descriptors are excluded by type.

`AsRecord[Tag](record, name)` creates an ordinary alias. Model inputs with no
outer clauses can use a direct table alias; filtered/windowed models and declared
projections retain a derived SELECT. Existing join functions and `Scope` work on
these aliases. Use `expr.At` for model fields and `rows.At` for projection fields.

```go
summary := rows.Select(rows.From(Orders.Query()), Summary,
    rows.Map(Summary.Amount, expr.Value(Orders.TotalCents)))
a := rows.AsRecord[summarySide](rows.RecordOf(rows.Limit(summary, 10)), "summary")
result := rows.SelectRecord(a, rows.Scope(a)).
    OrderBy(rows.At(rows.Scope(a), Summary.Amount).Asc())
items, err := result.All(ctx, db)
```

`SelectRecord(source, scope)` selects every field from one preserved source. Its
result retains the input scope, and exposes `Where`, `OrderBy`, `Limit`, `Offset`
and `All`. A nullable join side requires an explicitly nullable row declaration
and `rows.Select`. Both the type boundary and shared compiler reject selecting a
nullable side as a complete record. Foreign declarations with the same Go type
or alias fail validation.

Complete model record reads use the existing hydration and retrieval pipeline.
Rows close before retrieval hooks/observers run, using the actual executor and
its observer ownership. Empty, partial, canceled or unsuccessfully closed reads
skip retrieval dispatch. Callback failures return no partial result. Declared
projection records have their own codec and do not trigger model retrieval.
Eager-loading requests remain excluded from record adaptation.

`TransactionOf(record)` explicitly lifts an ordinary record.
`LockedRecord(modelQuery.ForUpdate())` retains a model lock and its wait policy.
`AsTransaction[Tag](record, name)` gives the transaction record an alias, and
`TransactionScope(alias)` exposes its fields. These types have no ordinary source
getters and cannot convert to ordinary records or aliases.

```go
claim := rows.AsTransaction[claimed](
    rows.LockedRecord(Orders.Query().ForUpdate().SkipLocked()), "claim")
scope := rows.TransactionScope(claim)
complete := rows.SelectTransactionRecord(claim, scope)
items, err := complete.All(ctx, tx) // requires *database.Tx

summary := rows.SelectTransaction(claim, Summary,
    rows.Map(Summary.Amount, expr.At(scope, Orders.TotalCents)))
rows, err := summary.All(ctx, tx)
```

`SelectTransaction` shares ordinary complete projection validation.
`SelectTransactionRecord` shares complete record selection. Both return
`TransactionRead`, with input-scoped filters, ordering, limits and offsets and a
transaction-only `All`. SQL, record metadata and executor kind travel in one
private getter; promoted methods cannot borrow a different source's eligibility.
No capability token is passed to application callbacks.

CTEs, recursive CTEs, sets and result cursors build on these adapters in later
Stage 6c slices. Pagination, count/first/stream terminals, scalar transaction
reads and explicit projection lock clauses also remain for those slices.
Transaction joins/correlation/LATERAL belong to 6b4 after the 6c foundations.
