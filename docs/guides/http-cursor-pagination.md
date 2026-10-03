# Typed HTTP cursor pagination

`DefineCursor` uses the normal ORM cursor paginator with generated domain filters
and an explicit response DTO. The original model or projection type remains the
cursor owner after mapping; it is separate from the public DTO type.

The [independent consumer](../../tests/fixtures/consumer/httppagination/cursor.go)
contains this complete handler pattern:

```go
type CursorListRequest = pagination.CursorRequest[
    foundryhttp.NoPath, MemberFilters, mutatorqueries.Member]
type CursorListResult = pagination.CursorResult[
    mutatorqueries.Member, MemberResponse]

var CursorList = pagination.DefineCursor[mutatorqueries.Member](
    foundryhttp.DefineRoute(
        foundryhttp.RouteSpec{ID: "members.cursor", Method: foundryhttp.GET, Access: foundryhttp.Public},
        foundryhttp.StaticPath("/members/cursor"),
    ),
    MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultCursorConfig(),
)

func (s DatabaseService) CursorList(ctx context.Context, in CursorListRequest) (CursorListResult, error) {
    page, err := memberQuery(in.Filters).CursorPaginate(ctx, s.DB, in.Page)
    if err != nil {
        return CursorListResult{}, err
    }
    return pagination.MapCursorPage(ctx, page, PresentMember)
}
```

Register `CursorList.Handle(service.CursorList)`. Foundry owns input parsing,
defaults, validation, response metadata and links. The shared domain query helper
applies filters once for both numbered and cursor pagination. `PresentMember`
explicitly selects typed model getters; stored IDs and fields remain unchanged.

For guarded reads, use the shared [authenticated pagination adapter](http-pagination.md#authenticated-page-reads); it preserves the concrete actor separately from the cursor source and public DTO.

## Requests and query ownership

Defaults are `after`, `before`, `per_page=20` and maximum size 100. Configure their
names and bounds with `CursorConfig`. Omit both cursor directions for the first
page. A supplied empty or malformed token fails decoding; supplying both valid
directions fails validation. Explicit invalid sizes are rejected rather than
replaced or clamped. Ordinary endpoint query-byte and pair limits still apply.
The size carries the [page size label](http-pagination.md) `http.pagination.size`.

`in.Page` is `query.CursorRequest[Source]`, directly usable by the model paginator.
For projections, joins, CTEs and complete result queries, declare their actual
result type as `Source` and use [CursorFor](result-cursor-pagination.md) in the
service. Both paths preserve the same source-owned tokens through DTO mapping.
Changing the requested size is permitted within endpoint limits.

`CursorQuery[Source]()` exposes the same codec for custom typed query declarations.
Parsing checks bounded token structure. Actual query execution additionally
checks the model/result owner, query fingerprint and ordered key codecs.
Changing filters or ordering invalidates an earlier query's cursor. Cursor input
and query-scope failures retain `query.CursorInputError`; the adapter returns HTTP
400 for those errors. Invalid query definitions and infrastructure failures stay
server errors. Pagination size/direction rules use the ordinary validation response.
The adapter bounds inspection of service errors, so a cyclic cause cannot retain
request ownership. Custom error methods still must return; matched cursor input
errors retain their existing 400 response.

Custom database codecs must define both encode and decode functions; `Validate`
checks their declaration without running either callback. Record-field metadata
rejects incomplete codecs. A decoder uses `fault.Invalid` for an invalid key
representation. Other errors, panic and Goexit remain server failures, preserving
private causes instead of publishing them in the response.

## Results and links

`MapCursorPage` validates the source page and returns `CursorResult[Source, DTO]`.
Its private metadata retains the original typed cursors without holding the source
model rows. `Items()` returns a slice copy; `Size()`, `Next()` and `Previous()` allow
inspection with the original source type. The zero result is invalid. Mapping
failure returns no partial result, and cancellation never abandons an active mapper.

The HTTP response contains `data`, `meta.per_page` and nullable `links.next` /
`links.prev`. There are no fabricated counts or page numbers. Empty data is `[]`
and empty pages have no navigation links. A service result must match the requested
size before success bytes are sent. `CursorJSON(DTOJSON())` exposes the same concrete
response schema used by runtime encoding and later contract export.

Next links use the returned `Next` cursor as `after`; previous links use `Previous`
as `before`. Each link carries only its direction, preserves domain filters and
uses the current route scope and page size. Default links are relative. Select
`PublicLinks` with [approved public origins](http-public-urls.md) for absolute URLs.
`URL(ctx, path, filters, request)` follows the same typed declarations and policies.

The [ORM cursor contract](model-pagination.md#typed-transport-boundary) owns token
format and consistency rules. Tokens include ordered key values and are neither
credentials nor encrypted data. Choose order fields suitable for transport, and
apply authorization on every request; the cursor does not grant access or provide
a snapshot across concurrent writes.

[Numbered/simple pagination](http-pagination.md) uses the same endpoint composition,
error declarations, middleware, validation, URL policy and DTO boundaries.

Custom database cursor-codec errors use the same bounded inspection. A reached
invalid-value marker retains its client-input classification; an incomplete
search remains a server failure and publishes no cursor boundary.
