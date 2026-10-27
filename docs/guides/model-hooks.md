# Typed model hooks

Declare a per-write factory returning `orm.Hooks[M]` and attach it with `orm.WithHooks` in the options passed to `orm.Define`.

<!-- orm-example: hooks-after -->
```go
func memberHooks() orm.Hooks[hooks.Member] {
	return orm.Hooks[hooks.Member]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[hooks.Member]) error {
		if !orm.WriteIsSet(draft, hooks.Members.Name.Set) {
			return orm.WriteSet(draft, hooks.Members.Name.Set("guest"))
		}
		return nil
	}}
}
```

<!-- orm-example: write-options -->
```go
func writeOptions() []orm.Option {
	return []orm.Option{orm.WithHooks(memberHooks)}
}
```

The [member declaration](../../tests/fixtures/goway/models/hooks/models.go) shows the complete attachment. The factory runs once per ordinary write inside the transaction, after an existing model has been locked and read. Closures can hold operation-local state. Query construction, metadata inspection and ordinary reads do not instantiate write factories.

## Provider observers

Register observers in a provider's registration callback for the same pool key used by the database module:

<!-- orm-example: register-observer -->
```go
func registerMemberObserver(r *foundation.Registrar, pool foundation.Key[*database.DB]) error {
	return orm.RegisterObserver(r, pool, orm.NewObserver[hooks.Member]("members.defaults"),
		func(foundation.Resolver) (func() orm.Hooks[hooks.Member], error) {
			return memberHooks, nil
		})
}
```

The constructor runs at application construction and can resolve injected services. Its returned factory runs once per write. Registration belongs to the application instance and pool, including sessions and nested transactions. Models need no local factory to dispatch registered observers. Do not register the same observer through two assembly paths.

Within each stage, local hooks run first, followed by observers in provider dependency/registration order. All `Saving` callbacks precede all `Creating` or `Updating` callbacks. Transactional failure stops subsequent stages. `orm.NewDeletionObserver[M]` restricts factory construction to deletion operations. `lifecycle.CountObservers[M]` exposes read-only registration counts without constructing or exposing factories.

## Callback contracts

| Callback | Arguments after `context.Context, *database.Tx` |
| --- | --- |
| `Saving` | `value.Optional[M], *orm.Write[M]` |
| `Creating` | `*orm.Write[M]` |
| `Updating` | `M, *orm.Write[M]` |
| `Deleting`, `Restoring`, `ForceDeleting` | `M` |
| `Created`, `Updated`, `Deleted`, `Saved`, `Restored`, `ForceDeleted` | `orm.Changes[M]` |

All callbacks return `error`. `AfterCommit` instead takes `context.Context`, `lifecycle.Operation` and `orm.Changes[M]`, without a transaction. Nil callbacks are skipped.

Create runs `Saving`, `Creating`, conventions, mutators/encryption, SQL, `Created`, then `Saved`. Update locks the stored model, then runs `Saving`, `Updating`, conventions, transforms, SQL, `Updated`, then `Saved`. Ordinary physical deletion runs `Deleting`, SQL, then `Deleted`. [Soft-delete stages](model-soft-deletes.md#lifecycle-time-and-failures) are separate.

## Read and change the pending draft

`orm.WriteGet(write, Column.Set)` reads an optional typed input. `WriteNullableGet` preserves omitted, NULL and value separately. `WriteIsSet` tests assignment presence. `WriteSet` replaces pending assignments and `WriteUnset` restores omission:

<!-- orm-example: unset-after -->
```go
func unset(draft *orm.Write[hooks.Member]) error {
	return orm.WriteUnset(draft, hooks.Members.Note.Set)
}
```

The field's setter retains model and input types. Drafts belong to their live before-write callback; do not retain them or the transaction. Caller assignments remain unchanged. Before hooks can supply required values or populate an empty update. Framework UUID preparation precedes creation hooks.

Foundry validates the final assignment shape after hooks and conventions, before invoking mutators or encryption. A hook cannot patch an existing primary key. Mutators run once on assigned values; NULL clearing skips scalar mutators. Returned models and changes come from stored values, including database defaults and trigger changes.

## Transactions and ownership

Callbacks run with result rows closed. Use their context and actual transaction for related writes. Propagate that context to retain cancellation and the bounded lifecycle nesting count. Do not start concurrent work on the transaction or perform irreversible external effects in transactional callbacks.

Errors, panic, `runtime.Goexit` and cancellation abort the write scope. Application errors leave the callback boundary in safe framework diagnostics that retain `errors.Is`/`errors.As` identity. Error formatting does not expose application or model text.

A normal nested write owns a savepoint. Rollback discards its changes and queued after-commit work. Success transfers callbacks to the parent, and only the outer commit runs them. After-commit failure cannot roll back persistence and does not skip later queued callbacks. The commit context belongs to the outer transaction; nested request values are not implicitly merged into it.

Local hooks and observers return failures through a safe `database.AfterCommitFailed` error with a committed outcome. The wrapper preserves `errors.Is` and `errors.As` identity without invoking the cause's formatting methods, even when its `Error()` would panic or call `runtime.Goexit`.

The existing [model write outcomes](model-writes.md) apply: an uncertain commit or an after-commit failure from a write that owns the outer transaction retains a typed reconciliation candidate. A write inside a caller-owned transaction returns before that transaction's final outcome; the caller must handle its outer transaction error. No write or callback is automatically retried. In-process callbacks do not provide crash durability.

No callback is automatically retried. Unknown commit outcomes require reconciliation through the [write outcome contract](model-writes.md#transaction-ownership-and-failures). In-process callbacks have no crash durability. Use [outbox](outbox.md) and [audit](audit.md) in the supplied transaction for persisted effects.

## Set and per-model behavior

Set updates/deletes skip per-model factories and require the applicable `WithoutModelHooks` acknowledgement. Insert batches, upserts and source writes explicitly skip these callbacks without a separate acknowledgement. Normal writes never silently switch to set behavior. [Per-model batches](model-batch-writes.md) preserve callbacks for every model inside one bounded atomic operation. Retrieval uses a separate factory and observer family described in [retrieval events](model-retrieval.md).

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
