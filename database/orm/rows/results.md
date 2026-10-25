# Completed-result cursors and read terminals

Use `CursorFor` after a complete model, projection, CTE or set selection. Its
scope describes output fields, independently of the original input scope:

```go
c := rows.CursorFor(rows.ModelRecord(Orders.Query().Limit(200)))
id := expr.At(c.Scope(), Orders.ID)
c = c.OrderBy(id.Asc()).UniqueBy(expr.Group(id))
page, err := c.Paginate(ctx, db, query.CursorRequest[Order]{Size: 20})
```

Input filters, grouping, ordering and windows stay inside an allocated SELECT
boundary. `Where` filters the completed output. `UniqueBy` is an application
assertion that the chosen output columns identify a row under database equality,
including NULL equality; it neither deduplicates rows nor proves uniqueness.
Missing identity orders are appended ascending. Order only by plain output
fields with default NULL placement; select computed values into a declared row
first. Repeated, foreign, sensitive or uninitialized keys fail before SQL.

`ValueCursorFor(rows.Values(selected))` retains the scalar's exact codec and
nullability. Use `c.UniqueBy(c.Key())`; explicitly deduplicate before the cursor
when values repeat. `Value()` provides the completed scalar for filtering and
ordering. Cursor builders are read-only and cannot be reused as record sources.

Pass `page.Next` as `After`, or `page.Previous` as `Before`. Each request reads
size plus one rows and uses the existing scoped token and nullable keyset
compiler. Backward reads reverse the returned items into canonical order. Empty
pages carry no navigation tokens. A decode, close, lifecycle or token-encoding
failure discards the entire page. Tokens are positions, not authorization,
encrypted data or a snapshot; authorize every request. Sensitive codecs cannot
supply automatic cursor keys. Use the existing `query.CursorRequest` and
`query.CursorPage` transport types.

`Read(report)` adapts a row query to `All`, `First`, `RequireFirst`, `Count`,
`Exists`, `Paginate` and `SimplePaginate`. Use `ReadScalar`, `ReadRecord`, `ReadSet`
and `ReadValues` for their corresponding adapters. No methods are added to
`Query` or `Table`. Numbered pagination requires explicit outer ordering and no
outer limit or offset. Counts retain the selected window, grouping and distinct
semantics; simple pages use a lookahead without a count. Complete model reads
retain their retrieval lifecycle. These adapters collect complete batches;
use `rows.CompileRead(read)` or `rows.PreflightRead(ctx, read)` to inspect any
ordinary read adapter, and `rows.EachRead(ctx, executor, read, yield)` to iterate
with bounded memory. A `RecordRead` can be passed directly. `ReadScalar`,
`ReadRecord`, `ReadSet` and `ReadValues` retain their concrete result types.

Complete models with possible retrieval hooks/observers use batches of 100,
closing rows before constructing factories, dispatching retrieval and yielding
models. Each nonempty batch constructs factories once. Primary-only orders use
keyset continuation when the complete source preserves unique model keys.
Nested joins, sets and recursion use offsets to retain repeated model IDs;
other non-keyable orders also advance by offset. The
original window and nested SELECT boundaries remain intact. Batches do not share
a snapshot. A failed batch delivers no models; previously delivered batches
remain delivered. Early callback errors retain identity and stop further reads.
Unknown executor wrappers cannot suppress model callbacks.

Scalars, declared DTOs and models whose concrete owner proves callbacks absent
stream one decoded row at a time. Cancellation, panic and Goexit close the rows;
iteration waits for a running callback to exit. Existing projection inspection
and `rows.Each(ctx, executor, query, yield)` retain their signatures; see
[report reads](README.md#report-reads). Transaction-only sources are rejected by
these ordinary terminals before I/O.

`SelectTransactionValue(source, expr.Select(field))` keeps the scalar type and
requires `*database.Tx` for every terminal. `TransactionRead` and
`TransactionValue` expose `First`, `RequireFirst`, `Count` and `Exists` in addition
to `All`. There is no conversion to ordinary result sources.

Apply projection or scalar locks through package functions:

```go
selected := rows.Read(report)
locked := rows.ForUpdate(selected).Of(rows.Target(preservedScope)).SkipLocked()
items, err := locked.All(ctx, tx)
```

`ForNoKeyUpdate`, `ForShare` and `ForKeyShare` use the same boundary. For different
policies on different preserved inputs, use `LockRows` with `UpdateLock`,
`NoKeyUpdateLock`, `ShareLock` or `KeyShareLock` descriptors. Each descriptor has
`Wait`, `NoWait` and `SkipLocked`. Targets must retain their input scope and
runtime declaration. Nullable scopes cannot be targets. The shared lock compiler
rejects aggregate/distinct/window/set and other ineligible targets. Locked
results expose `All`, `First` and `RequireFirst`, require a transaction, and
provide no count, pagination, mutation or ordinary-source conversion. They can
enter `TransactionCTE`, retaining the lock inside its definition.

Transaction joins and their nullable record selection rule remain the deferred
6b4 integration. Result cursors accept ordinary sources only.

Cursor keys retain per-output sensitivity through projections, computed values,
scalar subqueries, aliases, CTEs and set operations. Either set operand can make
an output sensitive, even when the left input supplies the decoder. Mapping a
sensitive expression to an ordinary row field does not make it safe for a cursor
token. Sensitive ordering and identity fields are rejected before SQL execution;
an unrelated public output can still be used as the identity. The old
`database/query` result cursor adapters use the same provenance check.

Sensitivity is captured when typed fields and aggregate operands enter the SQL
expression tree, before arithmetic or conditional operators select a result
codec. This also protects legacy fields declared with a sensitive codec when
`query.Define` omits its optional model getters. Ordinary and relation sources
share the model's sensitivity metadata, including through and pivot inputs.
