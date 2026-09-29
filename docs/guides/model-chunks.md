# Bounded model iteration

Generated model queries support ordinary typed slices for both collected results and batches. `All` collects the whole selected window. `Chunk` processes that window in bounded `[]User` batches; `EachChunked` delivers each complete model from those batches. The [independent consumer](../../tests/fixtures/consumer/chunkqueries/chunks_postgres_test.go) exercises these APIs against PostgreSQL.

```go
err := models.QueryUsers().
    Where(models.UserFields().Status.Eq(models.StatusActive)).
    OrderBy(models.UserFields().Age.Asc()).
    With(models.UserRelations().Orders).
    Chunk(ctx, db, 100, func(users []models.User) error {
        for _, user := range users {
            if err := processUser(user); err != nil {
                return err
            }
        }
        return nil
    })
```

`processUser` is application behavior. Foundry owns query execution, decoding, eager loading and cleanup. Callbacks retain the concrete model type; supplying an order callback to a user query fails Go compilation. There is no collection wrapper or string field lookup.

## Choosing a traversal

| Method | Callback | Traversal and row ownership |
| --- | --- | --- |
| `Each(ctx, db, callback)` | `func(User) error` | Streams rows when nothing can run per model; uses `DefaultChunkSize` keyset batches when eager relations, aggregate slots or retrieval hooks/observers are selected, or the executor cannot prove observers absent |
| `Chunk(ctx, db, size, callback)` | `func([]User) error` | Keyset batches (offset only for unkeyable orders) with all rows closed before each callback |
| `EachChunked(ctx, db, size, callback)` | `func(User) error` | Individual callbacks over complete `Chunk` batches |
| `ChunkByID(ctx, db, size, callback)` | `func([]User) error` | Primary-key batches with all rows closed before each callback |
| `EachByID(ctx, db, size, callback)` | `func(User) error` | Individual callbacks over complete primary-key batches |

Sizes must be between one and `query.MaxChunkSize`; invalid values fail before database access. `DefaultChunkSize` is the framework's default for eager `Each`. No count query is issued. An exact full final batch requires one additional empty read unless an explicit total `Limit` has been reached; partial batches stop immediately. Empty windows never call the callback.

`Chunk` and `EachChunked` preserve the query's filters, ordering, initial `Offset` and total `Limit`. A missing primary-key tie-breaker is appended in ascending order. For example, `Offset(10).Limit(250).Chunk(..., 100, ...)` delivers at most 100, 100 and 50 models, starting at the selected offset. The original query remains unchanged.

After the first batch, each batch continues after the last delivered row's ordered values and primary key (keyset traversal), so deleting or inserting rows behind the current position does not shift later batches, and the cost of a batch does not grow with its position. When every ordered field is declared NOT NULL and all share one direction, the continuation is one row comparison such as `("rank", "id") > ($1, $2)`, which a composite index on the same columns serves as a single range. Nullable fields use the NULL-aware expanded predicate (ascending NULLS LAST, descending NULLS FIRST). Orders that cannot be keyed fall back to `LIMIT`/`OFFSET` batches: computed expressions, explicit `NullsFirst`/`NullsLast` placement, and fields without a generated getter. Complete-model projections and set results, which `Each` batches when retrieval hooks may run, continue by primary key when ordered only by it over plain columns without joins, grouping or `DISTINCT`, and use offsets otherwise.

Offset batches over large offsets can become expensive, and changes to earlier matching rows can shift them. [PostgreSQL LIMIT/OFFSET behavior](https://www.postgresql.org/docs/18/queries-limit.html) explains both ordering and offset costs. Keyset batches still observe rows whose ordered values change during iteration; prefer primary-key traversal when changing the query's filter or order fields during iteration:

```go
u := models.UserFields()
err := models.QueryUsers().
    Where(u.Status.Eq(models.StatusActive)).
    EachByID(ctx, db, 100, func(user models.User) error {
        _, err := models.QueryUsers().Update(ctx, db, user.ID,
            models.UserDraft{}.SetStatus(models.StatusDisabled))
        return err
    })
```

`ByID` means the model's declared primary key, including natural keys. Ordering defaults to primary-key ascending; an explicit descending primary-key order is also supported. Other orderings and a nonzero `Offset` fail instead of being silently replaced. A query `Limit` still caps the total number delivered. For example, `QueryCountries().OrderBy(CountryFields().Code.Desc()).Limit(250)` traverses the typed country code in descending order.

The next key is encoded and retained before your callback runs, so changing a returned model's ID or rearranging its slice cannot redirect the next query. Keep primary keys stable in the database. Key traversal avoids offset shifts, but it does not freeze the matching set: new rows beyond the current boundary may be visited, rows moving behind it may be missed, and changing stored keys can cause skips or repeat visits. Use an appropriate caller-owned transaction/isolation level when a common database snapshot is required. These helpers do not provide durable checkpoints or exactly-once processing.

## Eager loading, memory and failures

Each batch is fully decoded, its parent rows closed, and its requested relations/aggregate slots loaded before publication. The same single-connection transaction can therefore run sequential queries inside a chunk callback. Plain `Each` without eager loads still owns an open row stream during callbacks; use a chunk method when callbacks need that transaction's connection.

Relation limits apply to each batch independently, including nested fetched and attached values. They bound related expansion alongside the parent chunk size; they are not a total-work budget. Use a total `Limit`, cancellation or callback error to stop the traversal. Querying relations separately inside every model callback can still create N+1 work; declare `With` to batch those loads.

The framework does not recycle returned slices. You may retain or reorder them, but retaining batches also retains their memory. Row limits do not cap the byte size of a single model field. Normal Go shallow-copy rules still apply to pointers, maps and nested slices in your models.

Callbacks run sequentially on the caller's goroutine. Callback errors retain their identity and stop iteration; panics propagate after database rows have already closed. Context cancellation stops database work and is checked between model callbacks. A failed decode, eager load or row cleanup publishes none of that batch. Earlier callback side effects are not automatically undone; use an explicit transaction for database rollback or design external effects to tolerate partial processing. A transaction's own cancellation/error rules still apply.

Read-only projections and set queries retain their existing streaming `Each` methods. This guide's chunk helpers apply to model queries with generated model metadata.

## Verification

Runtime and PostgreSQL tests cover retained windows, zero limits, primary-key ordering, natural keys, callback slice mutation, filter updates, closed rows, eager query counts and budgets, failed batches, cancellation and error/panic propagation. Consumer compiler checks reject mismatched model callbacks. Fresh generation and real gopls checks cover the public typed methods.
