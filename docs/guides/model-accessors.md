# Explicit model accessors

Call `Access<Field>` methods explicitly when mapping stored values into response DTOs. Model hydration, queries and JSON serialization do not call them automatically or replace stored fields with their results.

<!-- orm-example: accessors-after -->
```go
func accessor(member mutators.Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}
```

The [compiled fixture](../../tests/fixtures/goway/models/mutators/models.go) keeps its stored email separate from the accessor's `EmailLink` result. Nullable accessors can return a concrete nullable presentation type, preserving NULL separately from an empty value. Accessors never replace relation keys, cursor values, identity or lifecycle snapshots.

## Method contracts

On declared models, `Access` is reserved for persisted-field getters. Use a value receiver, no arguments, one concrete result and `error`. Startup rejects orphan fields, pointer receivers, additional arguments, missing errors and interface results. Other computed methods can use ordinary domain names without claiming a persisted field.

Unlike mutators, accessors receive the actual stored model. Keep them free of hidden database I/O. A direct call has ordinary Go error/panic semantics; the framework does not implicitly supply a transaction or callback isolation around application method calls.

## Notices beside the actual field

Document storage, mutation input and accessor behavior beside the handwritten model field and table declaration. Those are the source of ordinary gopls hover and definition information. No generated per-field method or managed-comment regeneration step is required.

Use [typed input columns](model-mutators.md) for automatic write transformations and [retrieval hooks](model-retrieval.md) for explicit post-hydration callbacks. Neither substitutes for DTO mapping. Persistence models are not implicitly public response contracts.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
