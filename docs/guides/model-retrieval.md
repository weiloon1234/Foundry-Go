# Typed model retrieval events

Complete-model reads can invoke typed `Retrieved` callbacks after hydration. This
is separate from [explicit getters](model-accessors.md): retrieval does not call
`Access<Field>` methods or replace stored fields with presentation values.

Declare a local factory alongside the handwritten model:

```go
//foundry:model table=users retrieval=readHooks
type User struct {
    ID   model.ID[User]
    Name string
}

func readHooks() UserRetrievalHooks {
    return UserRetrievalHooks{
        Retrieved: func(ctx context.Context, executor database.Executor, user User) error {
            // Scalar reads do not recursively dispatch model retrieval events.
            _, err := QueryUsers().Count(ctx, executor)
            return err
        },
    }
}
```

The excerpt uses `context` and Foundry's `database` and `model` packages. The
factory takes no arguments and returns the generated `UserRetrievalHooks`.
Generation rejects misspelled callbacks, incompatible model arguments and wrong
factory results. `hooks=writeHooks` and `retrieval=readHooks` may coexist; read
and write factories remain separate.

## Provider observers

Every generated model provides `<Model>RetrievalObserver`,
`New<Model>RetrievalObserver` and `Register<Model>RetrievalObserver`, including
models without a local factory. They use the same provider registration, typed
pool key and constructor resolver as [write observers](model-hooks.md#provider-observers):

```go
RegisterUserRetrievalObserver(
    registrar, pool, NewUserRetrievalObserver("users.read"),
    func(resolver foundation.Resolver) (func() UserRetrievalHooks, error) {
        return readHooks, nil
    },
)
```

The constructor can resolve injected services during application construction.
Its returned factory runs for reads. Observer names are unique across both read
and write registrations within a pool. Local callbacks run first, followed by
registered callbacks in provider/registration order. A nil callback is skipped.

## Hydration, lifetime and failures

Foundry hydrates and closes the complete fetch before constructing callbacks.
Empty fetches and failed hydration/row closure do not invoke factories. Each
selected model role constructs factories once per nonempty fetch or batch, then
dispatches callbacks in row order. Through relations hydrate both target and
pivot models and dispatch those roles in that order.

Callbacks receive the same executor supplied to the read, retaining wrappers
and their capabilities. Rows are closed before callback I/O, allowing further
queries through the same transaction/session. The callback context retains
request values, observes query/owner cancellation, and expires after callback
completion. Propagate it to nested work. Read and write callbacks share
`query.MaxLifecycleDepth`; recursive hooks fail at that bound.

Treat callback models as read-only snapshots. Ordinary Go value copies preserve
scalar fields; slices, maps and pointers still follow Go's shared-reference
rules. Getter evaluation and DTO mapping remain explicit application choices.

Errors, panics, `runtime.Goexit` and cancellation stop dispatch and discard the
pending collection/page. Earlier callback effects are not automatically undone
for a pool read: it has no implicit write transaction. When reading inside an
explicit transaction, propagate the error from its owning callback to roll back
that unit of work. These callbacks are in-process behavior, not durable events.

## Queries and bounded iteration

`All`, `First`, `Find`, locked reads, complete-record selections, aliases, CTEs,
sets, pages and relation hydration carry the model's retrieval adapter. Set
results use their first input's decoder and lifecycle declaration; their SQL
inputs do not dispatch independently. Explicit `Load`/`LoadMissing` loads related
models without dispatching retrieval again for supplied parent models.

Scalar reads such as `Count`/`Exists`, declared DTO projections, raw row helpers,
write snapshots and SQL `RETURNING` do not dispatch model retrieval callbacks.
Pagination lookahead rows are hydrated models and can invoke callbacks even
when subsequently trimmed from the returned page.

`Each` uses bounded offset batches when retrieval callbacks may run, closing
rows before callbacks. Complete-record iteration uses the same
`query.DefaultChunkSize` as model iteration and retains the selected query
window. Unknown executor wrappers cannot prove callbacks absent. Concrete owners
without applicable callbacks retain ordinary streaming where no eager load is
requested.

Batches do not establish a shared database snapshot. Changes to selected
membership/order can shift later offset results. Model `EachChunked` selects an
explicit batch size; `EachByID`/`ChunkByID` provide the existing primary-key
traversal contract. `All` intentionally collects its selected result; apply
`Limit` when the collection itself must be bounded.

The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records
verification status and the remaining lifecycle work.
