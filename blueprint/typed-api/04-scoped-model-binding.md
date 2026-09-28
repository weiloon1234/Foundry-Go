# T04 — Alternate keys and nested scoped model binding

Prerequisites: T01–T03. Status belongs to the
[master](../00-master-architecture-and-parity.md#typed-api-delivery).

## Actual gap

[ByKey](../../http/modelbinding/key.go) already selects a named typed path field.
[Define](../../http/modelbinding/resolver.go) already handles slug/parent filters,
as the [consumer](../../tests/fixtures/consumer/httpmodels/bindings.go) demonstrates.
The gap is repeated consumer code for alternate-field queries, loading a parent,
resolving children through it and delivering all resolved models to one handler.

For `/teams/{team}/projects/{project}`, the consumer should declare typed path
selectors and a typed relationship/key once. A successful handler receives a
concrete team and project alongside the original prepared input. It should not
repeat the parent query, compare arbitrary string field names or cast context data.

## Reusable composition

Extend `http/modelbinding` over existing resolvers, generated query fields and
relation descriptors. Provide an alternate-key resolver using a model-owned typed
field and path selector, plus parent/child composition using the resolved parent.
Return a concrete typed bundle/chain result; adding another level must preserve
every model type without a heterogeneous map or fixed global request registry.
Use generic package functions/generated concrete methods where needed.

The ordinary alternate route key must be unique inside its declared scope.
Reuse a declared uniqueness contract or detect more than one match as an internal
configuration/data error; do not silently select the first duplicate slug.
Primary-key lookup keeps its existing behavior. Relations retain filters, eager
loading, soft-delete scopes, hooks and caller-selected database executor.

Resolve a parent once per accepted request and pass that loaded value to the child
scope. A child outside the parent is 404, not a global fallback lookup. A missing
parent short-circuits children. Malformed path keys fail before SQL; DB/hook errors
never turn into not-found. No partial bundle reaches a handler. Each request owns
its result and cancellation; no cross-request model cache is introduced.

Authorization remains explicit. T03 request authorization may run before lookups;
a typed resource policy consumes the bound bundle and concrete actor afterwards.
Matching a relationship does not authorize access. Signed, authenticated and plain
endpoints preserve their existing descriptors, URL generation, status and errors.
Bound persistence models are not automatically response DTOs.

There is no implicit write transaction or lock. A handler making ownership-sensitive
writes must recheck/lock through its transaction if concurrent changes matter.
Support transaction executors where the underlying query contract supports them;
do not make a pool satisfy transaction-only lock requirements.

## Acceptance

- Independent nested routes use typed IDs and alternate slugs, with a second deeper
  relation and two parent records deliberately sharing the same child slug.
- A wrong-parent child, missing parent, soft-deleted row and policy denial never
  reach domain work; duplicate alternate keys cannot choose an arbitrary row.
- Count resolver calls to prove one parent resolution and no global child fallback;
  account separately for configured eager loads/retrieval hooks.
- Compiler rejection covers mismatched model-owned fields, relationship ownership,
  path value types, handler bundle and actor types. gopls exposes all bound fields.
- Native PostgreSQL tests preserve typed parameterization, filters and transaction
  semantics. Race/cancellation tests show no shared results or abandoned callbacks.
- Original `ByKey`, `Define`, `Bind` and authenticated/signed consumer fixtures remain
  green, and exported HTTP metadata still describes only the transport DTOs.

Use existing isolated fixtures during this milestone. T05 then replaces repeated
schema plumbing in a representative full HTTP case. Complete the common gate.

## Concrete T04 API

`ByField` composes the existing primary-key adapter with `query.Unique`.
`Through` loads a child using the parent's direct relation and unique stored key.
`ThroughSelected` adds another level while selecting its concrete parent from
the earlier `Models[Parent,Child]` bundle. `Then` provides a typed custom-child
composition boundary. Direct generated BelongsTo/HasOne/HasMany relationships
share `query.RelatedUnique`; pivot semantics require an explicit custom resolver.

`WithAuthorization` on bound plain/authenticated endpoints owns the post-binding
resource policy. Transport request authorization remains the earlier T03 stage.
No wire format changes or persistence-model serialization are introduced. See
the [guide](../../docs/guides/scoped-model-binding.md).
