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
issues. The HTTP kernel's body ceiling remains an additional input bound.

JSON input requires one Content-Type declaration: `application/json` or an
application media type with the `+json` suffix. A supplied charset must be UTF-8.
Compressed input is not implicitly decompressed by this adapter. Known oversized
bodies fail before reading; streamed bodies are read through bounded readers.

Malformed input returns 400; unsupported media returns 415; oversized bodies
return 413. Public field issues use JSON Pointer paths beginning with `/path`,
`/query` or `/body`. Unknown submitted names and received values are not echoed.
Internal causes remain available for diagnostics.

Custom readers, codecs and handlers finish before their owned resources are
released. Panics and Goexit become internal failures. Cancellation is checked
around the stages and after callbacks return; an uncooperative callback is not
abandoned. Native I/O timeouts and connection shutdown own blocked network I/O.
Timeout responses do not reverse domain work that has already committed.

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
