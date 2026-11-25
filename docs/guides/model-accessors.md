# Explicit model accessors

<a id="agent-entry"></a>
## Agent entry

- Map: [Explicit accessors](#recipe-access-field)

<a id="recipe-access-field"></a>
## Map stored values explicitly

Call `Access<Field>` methods explicitly when mapping stored values into response DTOs. Model hydration, queries and JSON serialization do not call them automatically or replace stored fields with their results.

`Member` is the application's model with a stored email and an `AccessEmail` method; `coremodels.EmailLink` is the application's presentation type. `emailLink` maps one member:

<!-- compiled-source: tests/fixtures/goway/models/mutators/guide_examples.go#accessors-after -->
```go
func emailLink(member Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}
```

The model keeps its stored email separate from the accessor's `EmailLink` result. Nullable accessors can return a concrete nullable presentation type, preserving NULL separately from an empty value. Accessors never replace relation keys, cursor values, identity or lifecycle snapshots.

## Method contracts

On declared models, `Access` is reserved for persisted-field getters. Use a value receiver, no arguments, one concrete result and `error`. Startup rejects orphan fields, pointer receivers, additional arguments, missing errors and interface results. Other computed methods can use ordinary domain names without claiming a persisted field.

Unlike mutators, accessors receive the actual stored model. Keep them free of hidden database I/O. A direct call has ordinary Go error/panic semantics; the framework does not implicitly supply a transaction or callback isolation around application method calls.

## Notices beside the actual field

Document storage, mutation input and accessor behavior beside the handwritten model field. Those are the source of ordinary gopls hover and definition information. Regenerate table handles after changing declarations; accessors remain explicit methods.

Use [typed input columns](model-mutators.md) for automatic write transformations and [retrieval hooks](model-retrieval.md) for explicit post-hydration callbacks. Neither substitutes for DTO mapping. Persistence models are not implicitly public response contracts.
