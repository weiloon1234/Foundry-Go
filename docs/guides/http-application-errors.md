# Application HTTP errors

**Status: focused runtime, consumer, compiler and editor acceptance passed.**

Declare a public error once and reuse the value in the endpoint and domain
operation. The declaration owns a semantic `ErrorCode`, a status from 400 through
599, and static public text. This is the same metadata the router exports.

```go
var SeatUnavailable = foundryhttp.DefineError(
    "seats.unavailable", 409, "The selected seat is no longer available.",
)

var Reserve = foundryhttp.DefineEndpoint(
    foundryhttp.DefineRoute(
        foundryhttp.RouteSpec{ID: "seats.reserve", Method: foundryhttp.POST, Access: foundryhttp.Public},
        foundryhttp.StaticPath("/seats/reserve"),
    ),
    foundryhttp.EmptyQuery(),
    foundryhttp.EmptyBody(),
    foundryhttp.EmptyResponse(204),
).WithErrors(SeatUnavailable)
```

The [consumer example](../../tests/fixtures/consumer/httpendpoints/errors.go)
registers a thin handler calling its domain service. Return `SeatUnavailable`
directly, or `SeatUnavailable.WithCause(err)` to retain an internal cause.
`errors.Is(err, SeatUnavailable)` checks the declaration and ordinary
`errors.As`/`errors.Is` can inspect wrapped causes. A cause's text never supplies
the public response message. Declarations and their wrapping errors format only
the public code. Wrapping and joining retain the existing outer/first
classification rules. Prefer declaration values over string comparisons.

A typed endpoint exposes only its declared application errors. An undeclared
custom error, including the same code with different metadata, becomes a safe
internal failure and is diagnosed in the request logger. The same check covers
middleware registered on that route. Built-in errors remain available through
[the shared error catalog](http-requests.md); they need no `WithErrors` entry.
`ErrorCode("seats.unavailable")` alone does not create a declaration.

`Endpoint.Description()` and `Router.Endpoints()` include application error
metadata. `Router.ErrorDefinitions()` returns a deterministic catalog containing
built-in and endpoint-declared errors. Returned metadata is owned; editing a
snapshot cannot change runtime behavior. Duplicate codes in one endpoint and
conflicting definitions across endpoints reject router assembly. Multiple
endpoints can reuse the same declaration. The final OpenAPI and TypeScript
exporters will consume these contracts in milestone 21.

In locale-enabled applications, a declared error's public message can be
translated. `SeatUnavailable.MessageDefinition()` returns its parameter-free
catalog signature, `http.error.seats.unavailable`; add it to
`application.FeatureDeclarations.Messages` and supply translations in `Catalog`,
as for the built-in `http.error.<code>` keys described in
[validation messages](validation-messages.md). Locales without a translation keep
the declared message. Code, status and exported metadata never change.

Zero/invalid declarations reject assembly and produce safe internal failures if
passed directly to `WriteError`. Public text must be nonblank, valid UTF-8,
NUL-free and at most 16KiB. Built-in codes are reserved. Declare only text that
is appropriate to expose, including for explicitly declared 5xx failures.
Arbitrary internal Go errors still use the generic internal-error response.
Localized message keys integrate in milestone 20.

Raw handlers may call `WriteError` explicitly with a valid declaration. Global
wrappers outside `Router` and raw handlers have no typed endpoint contract;
their responses do not acquire inferred endpoint error metadata. Model and
private-cause values never become fields in the public failure envelope.

The ordinary HTTP adapter preserves HEAD behavior, correlation and policy
headers, safe cache defaults, and ownership of custom error methods. Error
classification runs on the request goroutine and contains panics before
committing a response; `runtime.Goexit` in a custom error method ends that
goroutine.

Combined transport full regression passed.
