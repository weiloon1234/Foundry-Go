# Typed model retrieval events

`orm.RetrievalHooks[M]` dispatches `Retrieved` after complete-model hydration and row closure. It is separate from write hooks and explicit accessors. It does not replace stored fields with presentation values.

<!-- orm-example: retrieved-after -->
```go
func retrieval() orm.RetrievalHooks[reads.Member] {
	return orm.RetrievalHooks[reads.Member]{Retrieved: func(context.Context, database.Executor, reads.Member) error { return nil }}
}
```

Attach `orm.WithRetrievalHooks` through `orm.Define` options. Register provider observers for the same typed pool key:

<!-- orm-example: read-options -->
```go
func readOptions() []orm.Option {
	return []orm.Option{orm.WithRetrievalHooks(retrieval)}
}

func registerReadObserver(r *foundation.Registrar, pool foundation.Key[*database.DB]) error {
	return orm.RegisterRetrievalObserver(r, pool, orm.NewRetrievalObserver[reads.Member]("members.read"),
		func(foundation.Resolver) (func() orm.RetrievalHooks[reads.Member], error) {
			return retrieval, nil
		})
}
```

The [read fixture](../../tests/fixtures/goway/models/reads/models.go) contains the complete declaration. Provider constructors resolve services once during application construction. Each nonempty complete fetch or closed batch constructs local factories first, then observer factories in registration order. Empty results and hydration/closure failures construct none. Dispatch follows row order, with local callbacks before observers.

## Lifetime and failures

Rows close before callbacks, so callback I/O can reuse the supplied executor. The executor retains wrappers and transaction/session ownership. The context carries request values and cancellation, and expires after callback completion. Propagate it to nested work. Recursive retrieval and write callbacks share the bounded lifecycle depth.

Treat models as read-only snapshots. Slices, maps and pointers retain ordinary Go reference semantics. Errors, panic, `runtime.Goexit` and cancellation stop dispatch and discard the pending result. Safe diagnostics preserve error identity without formatting model values.

A pool read has no implicit write transaction, so earlier callback side effects are not automatically undone. For atomic related work, read inside a transaction and propagate callback failure from its owner. Retrieval callbacks are not durable events.

## Complete models and streaming

Model reads, complete-record selections, pages and relation loads carry retrieval metadata. Set results use the first input's decoder and lifecycle declaration; SQL inputs do not dispatch independently. Through relations dispatch target and pivot roles. `Load` and `LoadMissing` do not redispatch retrieval for supplied parent models.

`Table.Raw` hydrates complete models and dispatches retrieval, but bypasses all automatic scopes. Callers must supply tenant and visibility predicates themselves. DTO projections, scalars, `rows.Raw`, write candidate hydration and SQL `RETURNING` skip retrieval callbacks.

`Query.Each` and complete-record `rows.EachRead` use bounded closed batches when callbacks may run. Unknown executor wrappers count as potentially observed. Concrete owners with no callbacks can stream directly where no eager load is requested. Each batch constructs its own factories. Pagination lookahead rows can dispatch before being trimmed.

Offset batches do not share a database snapshot: concurrent changes can shift later results. Use the typed keyset traversal APIs when that traversal suits the operation. `All` collects its selected result, so apply a query limit when the collection must be bounded.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
