# Authenticated pagination acceptance — 2026-09-28

The starter's B05 gap was valid: pagination retained its HTTP endpoint privately,
so `http.Authenticated` could not bind it while preserving the typed page handler.
The framework now provides `pagination.Authenticated(endpoint, binding)` for
numbered, simple and cursor pages. This is an additive public API; existing
public pagination signatures and response formats remain compatible.

## Consumer contract

Configure a `Guarded` page declaration, then bind its concrete actor:

```go
secured := pagination.Authenticated(list, binding).
    WithScopes(readScopes).
    WithPermissions(readPermission).
    WithAuthorization(service.AuthorizeList)
registration := secured.Handle(service.List)
```

`WithAuthorization` is optional. Numbered/simple callbacks receive
`(context.Context, Actor, pagination.Request[Path, Filters])`; cursor callbacks
receive `pagination.CursorRequest[Path, Filters, Source]`. Their page return types
are unchanged. Exported `AuthenticatedNumberedEndpoint`,
`AuthenticatedSimpleEndpoint` and `AuthenticatedCursorEndpoint` aliases let a
consumer name the adapter in its own public signatures.

See the [usage guide](http-pagination.md#authenticated-page-reads),
[independent consumer](../../tests/fixtures/consumer/httppagination/authenticated.go)
and [implementation](../../http/pagination/authenticated_endpoint.go).

## Verification

The final native `make verify` passed in 539.71 seconds with existing
PostgreSQL/Redis required, real gopls and native Node/TypeScript selected. The gate
includes formatting, vet, framework/plugin/consumer tests, compiler rejection
cases, deterministic generation, documentation links and release-tool tests.
Fresh HTTP/authentication races and affected pagination/client consumer races
also passed. Six added compiler cases reject mismatched actors and authority;
two new real-editor scenarios verify constructor and handler usability.

Tests cover missing/invalid credentials, scope and permission denial before
decoding, validated request authorization before reads, actor delivery, page and
cursor validation, safe invalid-result/error handling, cancellation, typed DTOs,
links and actual manifest/OpenAPI security. The generated strict TypeScript client
performs anonymous rejection and authenticated HTTP calls for all three modes.

The initial full gate found a probe-location mistake: the constructor prefix also
matched an endpoint type alias. The correction selects the actual constructor
call; the focused editor checks and final full gate passed without changing the
runtime or weakening the expected completions.

Final source review retained the existing HTTP authentication owner and one shared
pagination completion path. The [evidence record](../evidence/authenticated-pagination-20260928.json)
contains commands, results, source fingerprints and this correction.

## Starter handoff

The starter's existing `Service.List(context.Context, identity.User, ListInput)`
already matches the new adapter. Its agent can declare a numbered `Guarded` route,
bind the existing identity guard/scopes/permission, register `Handle(s.List)`, then
regenerate its actual manifest and clients and test cross-owner HTTP listing.
No starter files were changed by this framework task. Application acceptance,
resolvable version selection, deployment and SDK design decisions remain separate
from this accepted framework adapter. No commit, push, merge or publication ran.
