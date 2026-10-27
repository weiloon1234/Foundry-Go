# Bounded model iteration

<!-- compiled-examples -->

Use `All` for a bounded collection, `Query.Each` for iteration or explicit model chunks
when callbacks must run after the current row stream closes. The
[consumer examples](../../tests/fixtures/consumer/chunkqueries/chunks_postgres_test.go)
exercise typed callbacks, windows, eager loading and failure boundaries.

## Choosing a traversal

| API | Delivery and ownership |
| --- | --- |
| `Query.Each(ctx, ex, visit)` | Direct stream when safe; bounded batches for retrieval/loading behavior |
| `Query.Chunk(ctx, ex, size, visit)` | `func([]M) error`, rows closed before callback |
| `Query.EachChunked(ctx, ex, size, visit)` | Individual models from closed, complete batches |
| `orm.ChunkByKey(ctx, ex, q, key, size, visit)` | Primary-key batches with an explicit typed key |
| `orm.EachByKey(ctx, ex, q, key, size, visit)` | Individual models from primary-key batches |

Sizes must be positive and no greater than `orm.MaxChunkSize`. Chunk preserves
filters, ordering, initial offset and total limit; a primary-key tie-breaker is
added when needed. Empty windows do not call back. No count query is issued.
An exactly full final batch can require an additional empty read.

Keyable ordering advances after the last delivered row's order values and primary
key. Nullable values use NULL-aware continuation. Computed orders, unsupported
getters or explicit NULL placement use offset batches instead. Primary-key helpers
require the declared key, only key ordering and no offset; an explicit descending
key order is allowed. Natural keys retain their exact type.

Key state is captured before callbacks can mutate a result slice. Keep database
keys stable: batches do not freeze membership, and concurrent updates can move
rows across a boundary. These helpers are not durable checkpoints or exactly-once
processing. Use an appropriate transaction/isolation level for one snapshot.

## Eager loading, memory and failures

Each batch is fully decoded and its parent rows closed before relations/aggregate
slots load and callbacks run. The same transaction can then do sequential work in
the callback. A direct `Query.Each` stream still owns its connection during callbacks;
use chunks for callbacks that need that connection.

Relation budgets apply per batch, including attached/nested results. A total query
limit, cancellation or callback error bounds total delivery. Returning a batch
transfers ordinary Go values; retaining batches retains memory, and shallow-copy
rules still apply to pointers/maps/slices. A row-count limit does not bound the
byte size of one model field. Per-row relation queries can still create N+1 work;
use declared eager loading when relations should be fetched in batches.

Callback errors stop iteration with their identity. Cancellation is checked
between deliveries. Failed decoding/loading publishes none of that batch, but
previous callback effects are not rolled back automatically. Callbacks run
sequentially; panics propagate after row cleanup.

## Projections and other read families

`rows.EachRead` streams DTO projections, scalar,
record and set read handles. Complete models that may run retrieval behavior use
bounded closed batches; DTO/scalar results stream directly. Ordering and windows
remain in their original SELECT. See [read terminals](model-projections.md#execution-and-failures).
Streaming an export must not first collect all results in a slice.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
