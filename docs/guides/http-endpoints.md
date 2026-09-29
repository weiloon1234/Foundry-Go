# Typed HTTP endpoints

**Typed endpoint and request-deadline APIs are available.** Focused runtime,
generation, consumer race tests, six new compiler-rejection cases and vet passed.
Canonical generation, consumer checks and actual editor probes also passed.
Full regression verification also passed. [Typed validation](validation.md) now
adds generated field rules and endpoint integration; its focused checks passed.
[Route-model binding](http-model-binding.md), [pagination adapters](http-pagination.md) and additional validation/middleware are available. Combined transport acceptance passed; the [HTTP blueprint](../../blueprint/08-http-validation-and-responses.md) records current milestone status.

An endpoint combines a route, query declaration, request body and response
descriptor. Generated descriptors retain the application types throughout
decoding, handler invocation, response encoding and inspection.

```go
type UpdateRequest = foundryhttp.Input[
    httpkernel.UserPath,
    foundryhttp.NoQuery,
    httpdto.UpdateUser,
]

var Update = foundryhttp.DefineEndpoint(
    foundryhttp.DefineRoute(
        foundryhttp.RouteSpec{
            ID: "users.update",
            Method: foundryhttp.PATCH,
            Access: foundryhttp.Public,
        },
        httpkernel.UserPathDescriptor(),
    ),
    foundryhttp.EmptyQuery(),
    foundryhttp.JSONBody(httpdto.UpdateUserJSON()),
    foundryhttp.JSONResponse(200, httpdto.UserResponseJSON()),
)

type UserService interface {
    Update(context.Context, UpdateRequest) (httpdto.UserResponse, error)
}

func Router(service UserService) (*foundryhttp.Router, error) {
    return foundryhttp.NewRouter(Update.Handle(service.Update))
}
```

`httpkernel` and `httpdto` are the independent fixture's application packages.
The consumer fixture additionally binds a generated query declaration. The
framework owns the wire handling; the service owns business behavior and database
transaction boundaries. The example's public route does not establish an
authentication policy. For a concrete authenticated model parameter, use the
[typed authentication adapter](authentication.md) with an explicitly guarded route.

`Input[P,Q,B]` preserves three distinct sources. A JSON field cannot replace a
path field with the same name. Its concrete path ID retains its model owner;
body optionals retain omission and explicit-null semantics. Registering a
handler with a different path, query, body or response type fails compilation.
Returning a persistence model does not automatically make it a response DTO.

## Payload contracts and URL generation

`JSONBody` uses the generated DTO's strict JSON contract. Unknown fields,
duplicates, wrong scalar representations, invalid enums and inappropriate nulls
are rejected before the handler runs. This is transport decoding; an accepted
email-shaped string still needs the application's declared business validation.

`EmptyBody()` has type `Body[NoBody]` and rejects request content. `EmptyQuery()`
has type `Query[NoQuery]` and rejects undeclared query keys. Typed GET and HEAD
endpoints require empty request bodies. Use the explicit raw adapter for native
HTTP behavior outside these typed payload contracts.

`JSONResponse` declares a body-bearing 2xx success status and a concrete JSON
response type. `EmptyResponse(204)` and `EmptyResponse(205)` require a handler
returning `NoContent`. Failed domain operations return ordinary Go errors;
Foundry's existing typed error codes determine their safe HTTP response.

`JSONResponses(descriptor, 200, 201)` declares several success statuses, primary
first (two to eight unique body-bearing 2xx statuses), for example an upsert.
The handler returns `Statused[R]{Status, Value}` and selects one; zero selects
the primary status. An undeclared status is a 500 and nothing is published.
Metadata (`EndpointInfo.Statuses`), OpenAPI and the TypeScript client expose
every declared status; the client returns `{ status, body }`. Idempotent
endpoints keep one status, because a replay reproduces it.

```go
var Upsert = foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(),
    foundryhttp.JSONBody(MemberJSON()), foundryhttp.JSONResponses(MemberJSON(), 200, 201))

registration := Upsert.Handle(func(ctx context.Context, in Request) (foundryhttp.Statused[Member], error) {
    member, created, err := members.Upsert(ctx, in.Body)
    if created {
        return foundryhttp.Statused[Member]{Status: 201, Value: member}, err
    }
    return foundryhttp.Statused[Member]{Value: member}, err
})
```

`RedirectResponse(303)` declares a bodiless redirect (301, 302, 303, 307 or 308),
typically after a browser form submission. The handler returns a `Redirect` from
`RedirectToRoute(route, path)`, `RedirectToEndpoint(ctx, endpoint, path, query)`
or `RedirectTo(location)`. Every target is a relative URL on this origin: it
starts with one `/` (never `//` or `/\`), uses printable ASCII without spaces or
backslashes and has no scheme or host, so request input cannot create an open
redirect. An invalid target is a 500. A browser session established by the
handler is published with the redirect. Metadata marks it `Redirect`; OpenAPI
describes the `Location` header and the TypeScript client returns the location
instead of following it.

`RawRequestBody("application/octet-stream", ...)` declares a streaming request
body of up to 16 media types. The handler receives a `RawBody`, an `io.Reader`
with `MediaType()` (the request's Content-Type) and `Length()` (the declared
Content-Length, unset when chunked). Nothing is buffered in advance. A request
whose Content-Type does not match a declared media type, or whose encoding is not
identity, is 415 before any hook runs; a declared length above
`EndpointLimits.Raw.Bytes` (default 2 MiB) is 413. Reads stop at that limit and
the route body limit; read errors are framework errors the handler can return
as-is (413 oversized, 408 too slow, 400 broken transfer). The body closes when
the handler returns. Preparation, authorization and validation receive it but
must not read it. For large uploads raise `EndpointLimits.Raw.Bytes`, the route's
`WithBodyLimit` and `WithTimeout` together. OpenAPI publishes binary content per
media type, and the TypeScript client sends a `Blob`, `ArrayBuffer`, typed array
or `ReadableStream`.

```go
location, err := Update.URL(ctx, path, foundryhttp.NoQuery{})
scoped := Update.Within(foundryhttp.DefineScope("/api", "api"))
```

`URL` combines the existing path and query codecs, escaping each source using its
own URL rules. The result is relative and does not use an incoming host. `Within`
shares route scope naming and path-prefix behavior.

## Bounds, failure and ownership

`DefaultEndpointLimits()` supplies query and JSON budgets; `WithLimits` returns
an independent endpoint declaration. Query limits bound bytes, pairs and issues.
Body and response JSON limits bound bytes, depth, wire nodes, traversal steps and
issues. Request bodies default to the kernel's 2 MiB body size. Responses default
to 32 MiB and 1,048,576 wire nodes (`DefaultResponseBytes`, `DefaultResponseNodes`),
enough for ordinary list pages. A response over its limits is a 500 whose
redacted diagnostic names `EndpointLimits.Response` and the exceeded byte, depth
or node bound. The HTTP kernel's body ceiling remains an additional input bound.

`WithHeaders(func(ctx, result R) ([]ResponseHeader, error))` derives response
metadata from the handler's successful result before the response is encoded,
for example a `Location` for a created resource with `RouteLocation(route, path)`
or `EndpointLocation(ctx, endpoint, path, query)`, which reuse typed URL
generation. Allowed names are `Location`, `Content-Location`, `Content-Language`,
`Cache-Control`, `Expires`, `Last-Modified`, `Link` and `Vary` (which adds to
framework values), plus application `X-*` headers other than proxy, framework
and security headers such as `X-Accel-*`, `X-Sendfile`, `X-Forwarded-*`,
`X-Request-Id` and `X-Frame-Options`. Values must be valid `HeaderValue`s; only
`Link` and `Vary` may repeat, and at most 16 headers apply. A returned error or a
rejected header is a 500 and no success is published. JSON and empty responses
support it; idempotent endpoints use `IdempotentEndpoint.WithHeaders`, whose
headers are replayed exactly. Headers never change the status; declare
alternatives with `JSONResponses`.

```go
created := endpoint.WithHeaders(func(_ context.Context, order OrderReply) ([]foundryhttp.ResponseHeader, error) {
    location, err := foundryhttp.RouteLocation(ShowOrder, OrderPath{Order: order.ID})
    return []foundryhttp.ResponseHeader{location}, err
})
```

`WithTimeout(d)` replaces the kernel `RequestTimeout` for one endpoint's route,
for example a long report or upload; a longer timeout also extends the native
connection read/write deadlines for that request. `WithBodyLimit(n)` replaces the
kernel `MaxBodyBytes` for that route only, so an upload endpoint can accept large
files without raising the server-wide default. Raise the matching endpoint payload
limit too (for example `EndpointLimits.Multipart.Bytes`). Both are applied after
route matching, are bounded by `MaxRouteTimeout` (24 hours) and `MaxRouteBodyBytes`,
and are reported by `RouteInfo.Timeout` and `RouteInfo.MaxBodyBytes`. They are
server policy and are not exported to client manifests.

```go
upload := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.EmptyResponse(204)).
    WithLimits(uploadLimits). // Multipart.Bytes/FileBytes: 512 MiB
    WithBodyLimit(512 << 20).
    WithTimeout(10 * time.Minute)
```

JSON input requires one Content-Type declaration: `application/json` or an
application media type with the `+json` suffix. A supplied charset must be UTF-8.
Compressed input is not implicitly decompressed by this adapter. Known oversized
bodies fail before reading; streamed bodies are read through bounded readers.

Malformed input returns 400; unsupported media returns 415; oversized bodies
return 413. Public field issues use JSON Pointer paths beginning with `/path`,
`/query` or `/body`. Unknown submitted names and received values are not echoed.
Internal causes remain available for diagnostics.

Custom readers, codecs and handlers finish before their owned resources are
released. Handlers, response preparation and application hooks (preparation,
authorization, file sources) run through an isolated boundary: panics and Goexit
become internal failures. Framework hot paths (path/query codecs, body reads,
URL generation and error classification) run on the request goroutine without a
per-call goroutine: a panic becomes an internal failure, while `runtime.Goexit`
ends that goroutine as any Go call does; the kernel still releases the request. Cancellation is checked around the stages and after callbacks
return; an uncooperative callback is not abandoned. Native I/O timeouts and
connection shutdown own blocked network I/O.

The handler's outcome is authoritative. A returned error, including a declared
application error, is published as returned even if the deadline expired. A
successful result is encoded (detached from cancellation, still bounded by its
limits) and written even if the deadline expired after the handler returned;
only an actual write failure can then prevent delivery. Credential-bearing
responses are the exception: token and MFA responses and browser-session cookies
are never published after the request context ended, even when issuance
completed, and the request fails with 503; the client obtains a fresh credential.
A deadline while the
request body, query or multipart form is still being read or decoded returns
408 `request_timeout`, because the client was too slow. A deadline after
decoding (preparation, authorization, validation, an unclassified handler error
or opening a file source) is the server's own budget and returns 503
`unavailable`. Timeout responses do not reverse domain work that has already
committed.

Responses are encoded and checked against their declaration before success
headers are written. Encoding failures return a safe internal error without
exposing output field issues. HEAD retains representation headers and omits the
body. A failed or short native write after commit aborts the request instead of
attempting a second response.

## Inspection

`Endpoint.Description()` and `Router.Endpoints()` expose owned snapshots of the
same JSON graphs used at runtime. Router snapshots are ordered by route ID. Raw
handlers remain in `Routes()` without acquiring guessed payload schemas.

Current endpoint metadata includes JSON bodies/responses and query cardinality.
Path/query scalar schemas reuse their typed codecs; the complete normalized client manifest remains required work. Model lookup uses the explicit binding adapter. Authorization and business validation do not follow merely from declared input types.

## Documentation and examples

`WithDocumentation(RouteDocumentation{Summary, Description, Tags, Deprecated})`
on a route or endpoint adds bounded, human-facing documentation to
`RouteInfo.Documentation`. It never changes matching, access or limits.
`WithBodyExample(value)` and `WithResponseExample(value)` encode a typed example
through the endpoint's own JSON contracts when declared; an example that fails
its contract, or an example for a non-JSON payload, makes `Validate` fail. The
encoded bytes appear as `PayloadInfo.Example`. Client exports publish both, as
described in [client contracts](client-contracts.md#operation-documentation-examples-and-servers).
