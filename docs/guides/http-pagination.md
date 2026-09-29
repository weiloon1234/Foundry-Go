# Typed HTTP pagination

Declare a paginated endpoint with the generated domain filters and an explicit
response DTO. Foundry parses page inputs, applies defaults and bounds, and builds
response metadata and navigation links. The service uses the normal ORM API.

The independent [consumer](../../tests/fixtures/consumer/httppagination/pagination.go)
contains the complete declarations and generated contracts for this example:

```go
var List = pagination.DefineNumbered(
    foundryhttp.DefineRoute(
        foundryhttp.RouteSpec{ID: "members.index", Method: foundryhttp.GET, Access: foundryhttp.Public},
        foundryhttp.StaticPath("/members"),
    ),
    MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultConfig(),
)

type ListRequest = pagination.Request[foundryhttp.NoPath, MemberFilters]

func (s DatabaseService) List(ctx context.Context, in ListRequest) (query.Page[MemberResponse], error) {
    builder := mutatorqueries.QueryMutatorMembers()
    if email, set := in.Filters.Email.Get(); set {
        builder = builder.Where(mutatorqueries.MemberFields().Email.Eq(email))
    }
    page, err := builder.Paginate(ctx, s.DB, in.Page)
    if err != nil {
        return query.Page[MemberResponse]{}, err
    }
    return pagination.MapPage(ctx, page, PresentMember)
}
```

`List.Handle(service.List)` registers the endpoint. `PresentMember` selects the
model's typed accessors explicitly and preserves the stored model ID. Mapping
errors, panic, Goexit and cancellation return no partial page. Mapping does not
modify stored fields or invoke write mutators. Ordinary Go ownership applies to
any pointers/maps a DTO deliberately retains from its source.

## Authenticated page reads

Declare the route with `Access: foundryhttp.Guarded`, configure pagination as
usual, and pass its concrete `foundryhttp.GuardBinding[Actor]` to
`pagination.Authenticated`. Numbered, simple and cursor endpoints use the same
adapter and infer all types from the endpoint and binding:

```go
secured := pagination.Authenticated(list, binding).
    WithScopes(readScopes).
    WithPermissions(readPermission).
    WithAuthorization(service.AuthorizeList)
registration := secured.Handle(service.List)
```

The numbered/simple handler is
`func(context.Context, Actor, pagination.Request[Path, Filters]) (Page, error)`;
`Page` remains `query.Page[DTO]` or `query.SimplePage[DTO]`. Cursor handlers use
`pagination.CursorRequest[Path, Filters, Source]` and return
`pagination.CursorResult[Source, DTO]`. Cursor source, actor and DTO ownership
remain separate. The [independent consumer](../../tests/fixtures/consumer/httppagination/authenticated.go)
also names the three exported `Authenticated*Endpoint` aliases in return types.

Authentication, required scopes and current-model permissions run before input
decoding. `WithAuthorization` receives the actor and the decoded page request
before validation and the read (the HTTP request lifecycle authorizes before
validating), using the ordinary HTTP hook isolation and error mapping. Apply resource
or tenant policy on every request; cursor tokens confer no authority. Configure
`Within`, middleware, limits, errors and path/filter validation before binding.
`Description`, actual route registration and exported client/OpenAPI security use
the same guard binding. URL building, defaults, links, DTO privacy, cancellation
and invalid-result handling share the existing pagination implementation.

## Inputs and limits

`DefaultConfig()` uses `page=1`, `per_page=20`, maximum size 100, maximum page
`pagination.DefaultMaximumPage` (10,000) and relative links. Set `NumberParam`,
`SizeParam`, `DefaultSize`, `MaximumSize` and `MaximumPage` once per endpoint.
The maximum size cannot exceed `query.MaxPageSize`. `MaximumPage` bounds how deep
an OFFSET scan a client can request (at most `MaximumPage × MaximumSize` skipped
rows); zero selects the default, and a deeper page fails validation with a 422 at
`/query/page`. Navigation omits a `next` link beyond the bound. Use
[cursor pagination](http-cursor-pagination.md) for unbounded traversal. Invalid
declarations and conflicting filter/page names fail router assembly. Explicit
zero/negative/oversized values are rejected, never replaced by defaults or
clamped. The ORM validates page number, size and offset representability without
database I/O.

The existing [query codecs](http-query-composition.md) own parsing, duplicate and
unknown-name checks. `WithFiltersValidation` reuses typed generated filter fields;
errors retain their actual `/query/<name>` path. Pagination bounds remain active
when additional filter rules are registered. Defaults and validation rules appear
in the same endpoint metadata used by later contract export.

## Responses and navigation

A numbered response has `data`, `meta` and `links`. Metadata contains
`current_page`, `per_page`, `total` and `last_page`. `next` and `prev` are nullable
URL strings. Empty data is `[]`; an empty total preserves the ORM's `last_page=0`.
Requests beyond the last page remain valid and can link backwards.

The result must retain the requested number and size, a consistent total/page
count and a bounded number of items. Invalid service results fail before success
bytes are sent. Counts and rows can observe different database states; use the
ORM's documented transaction isolation when a consistent snapshot is required.

`DefineSimple` expects `query.SimplePage[DTO]`. Use `SimplePaginate` in the service
and `MapSimplePage` to choose DTO values. Its metadata contains `current_page`,
`per_page` and `has_more`; it does not claim totals. `HasMore` requires a full page,
as defined by the ORM's lookahead read.

Links preserve declared filters and use the endpoint's current route scope and
query codecs. They are navigation hints, not guarantees that data will survive a
later request. `URL(ctx, path, filters, page)` uses the same declarations. Relative
links ignore arbitrary incoming Host values. For absolute links, select
`PublicLinks` and register [PublicURLs](http-public-urls.md) with approved origins.
A `PublicLinks` endpoint marks its route with `RequirePublicURLs()`, so
`ApplyMiddleware` rejects a router that lacks `PublicURLs` at assembly; a router
served without `ApplyMiddleware` answers such routes with a shared 500.

Two opt-in `Config` fields add navigation for page-number UIs. `EdgeLinks` adds
`first` and, for numbered pages, `last` (page `max(last_page, 1)`). `PageWindow`
(at most `pagination.MaximumPageWindow`, 10) adds `window`: the numbered pages up
to that distance on each side of the current page, clipped to 1 and `last_page`,
each as `{"page": n, "url": "..."}`. A request beyond the last page has an empty
window. Both are absent from the wire when disabled, so existing clients keep
the same envelope. No link points beyond `MaximumPage`: a deeper `last` is
omitted and the window stops at the bound. Simple pages have no count and only
receive `first`; cursor pages receive neither.

```go
config := pagination.DefaultConfig()
config.EdgeLinks = true
config.PageWindow = 2 // page 5 of 9 links pages 3–7
```

`NumberedJSON(item)` and `SimpleJSON(item)` expose typed response contracts. The
framework response fields/tags own the envelope; the supplied generated DTO owns
its item fields and codecs. Models do not automatically become public DTOs.
`Description()` exposes the assembled query, validation and JSON metadata used
by the shared [client contract exporters](client-contracts.md).

[Cursor HTTP pagination](http-cursor-pagination.md) uses the same transport composition while preserving model/projection ownership in navigation positions after DTO mapping.
