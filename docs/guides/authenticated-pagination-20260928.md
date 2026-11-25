# Authenticated pagination acceptance — 2026-09-28

The starter's B05 gap was valid: pagination retained its HTTP endpoint privately,
so `foundryhttp.Authenticated` could not bind it while preserving the typed page handler.
The framework now provides `pagination.Authenticated(endpoint, binding)` for
numbered, simple and cursor pages. This is an additive public API; existing
public pagination signatures and response formats remain compatible.

## Consumer contract

`MemberFilters` is a query struct with `Email value.Optional[string]` tagged `query:"email"`; `MemberResponse` is the response DTO with `ID model.ID[mutatorqueries.Member]`, `Email mutatorqueries.DisplayEmail`, and `Nickname value.Nullable[mutatorqueries.DisplayNickname]` (JSON names `id`, `email`, and `nickname`). The two display types are string-backed model presentation values. Generate the query and response descriptors; `list` is a `pagination.NumberedEndpoint[foundryhttp.NoPath, MemberFilters, MemberResponse]` declared with guarded access.

The application owns `Actor` and `mutatorqueries.Member` and any generated tables or identities used below.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `auth`, `http` as `foundryhttp`, `database/orm`, `http/pagination`.

Configure a `Guarded` page declaration, then bind its concrete actor:

<!-- compiled-source: tests/fixtures/consumer/httppagination/authorization_recipe.go#authorized-list-service -->
```go
type authorizedListService interface {
	AuthorizeList(context.Context, Actor, pagination.Request[foundryhttp.NoPath, MemberFilters]) error
	List(context.Context, Actor, pagination.Request[foundryhttp.NoPath, MemberFilters]) (orm.PageResult[MemberResponse], error)
}
```

Inputs: `list pagination.NumberedEndpoint[foundryhttp.NoPath, MemberFilters, MemberResponse]`, `binding foundryhttp.GuardBinding[Actor]`, `readScopes auth.AccessScopes[Actor]`, `readPermission auth.Permission[Actor]`, `service authorizedListService`.

<!-- compiled-source: tests/fixtures/consumer/httppagination/authorization_recipe.go#authenticated-pagination-20260928-1 -->
```go
secured := pagination.Authenticated(list, binding, foundryhttp.Scopes(readScopes), foundryhttp.Permissions(readPermission))
secured = pagination.AuthorizeActor(secured, service.AuthorizeList)
return pagination.HandleAuthenticated(secured, service.List)
```

`pagination.AuthorizeActor` is optional. Numbered/simple callbacks receive
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
pagination completion path.

## Starter handoff

The starter's existing `Service.List(context.Context, identity.User, ListInput)`
already matches the new adapter. Its agent can declare a numbered `Guarded` route,
bind the existing identity guard/scopes/permission, register `pagination.HandleAuthenticated(secured, s.List)`, then
regenerate its actual manifest and clients and test cross-owner HTTP listing.
No starter files were changed by this framework task. Application acceptance,
resolvable version selection, deployment and SDK design decisions remain separate
from this accepted framework adapter. No commit, push, merge or publication ran.
