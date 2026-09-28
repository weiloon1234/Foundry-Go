# HTTP request limits, attribution and errors

The [HTTP kernel](http-kernel.md) now assigns request identity, bounds request
bodies and provides a shared typed error response. [Typed endpoints](http-endpoints.md),
[DTO decoding](http-dtos.md), [validation](validation.md) and
[automatic pagination](http-pagination.md) reuse these transport boundaries.

## Request identity

Every request reaching the kernel handler receives a generated UUID correlation
ID. `http.RequestID(ctx)` returns the existing `attribution.RequestID` type; the
same value is available from `attribution.FromContext(ctx).Request().ID` and in
the `X-Request-ID` response header. Framework request diagnostics use that ID.
Incoming request-ID headers do not select it.

HTTP starts fresh anonymous attribution, retaining ordinary context values but
excluding an application-level model or system identity. It captures the direct
TCP peer and user agent through the existing bounded attribution contract.
Forwarding headers are ignored until an explicit trusted-proxy adapter is applied.
Request IDs are correlation metadata, never authentication credentials.

The user agent must satisfy the shared attribution bounds and text validation.
Invalid metadata returns `BadRequest` with the generated ID still present. Outside
an attributed operation, `RequestID` returns the empty ID. Background operations
can retain request provenance through the existing
[attribution contracts](model-references.md).

## Streaming body limits

`ServerConfig.MaxBodyBytes` is a positive global ceiling; its default is 2 MiB.
Increase it explicitly for larger accepted uploads. The limit does not allocate
a buffer of that size. It applies to both declared and unknown-length streams.

A declared oversized body is rejected before application handling or body reads.
On HTTP/1.x the rejection closes the connection, allowing a `100-continue` client
to receive the error without first sending its body. Unknown-length bodies use
standard `http.MaxBytesReader`, which reports `*http.MaxBytesError` when a read
crosses the limit. Body ownership lasts through handler completion and closure.

Raw `net/http` handlers must handle their read errors. They can use
`PayloadTooLarge.WithCause(err)` with `WriteError` for the shared rejection. The
framework cannot replace an HTTP response a raw handler has already sent or
infer success from an ignored read error. Typed endpoint decoding owns this
mapping before invoking the domain handler.

## Typed public errors

`http.ErrorCode` implements Go's `error` interface. Built-in values include
`BadRequest`, `NotFound`, `Forbidden`, `Conflict`, `PayloadTooLarge`,
`ValidationFailed`, `RequestTimeout`, `InternalError` and `Unavailable`.
`ErrorDefinitions()` returns the deterministic built-in code/status/message
catalog that response encoding uses. Returned entries cannot mutate the catalog.

`NotFound.WithCause(err)` retains the internal cause for `errors.Is` and
`errors.As`, with safe ordinary formatting. Public JSON uses the fixed catalog
message. An unknown code or ordinary internal error becomes a generic 500;
an internal `fault.Missing` does not become a public resource-not-found response.
The first explicit HTTP classification in an error chain takes precedence over
an internal classified cause.

`WriteError(w, r, err)` writes an `ErrorResponse` to a fresh response and returns
any encoding/write failure. Its JSON contains `status`, `error_code`, `message`
and the optional `request_id`. Header and JSON correlation come from the same
attribution. HEAD responses omit the body. Nil errors are rejected.

Each framework error search visits at most 256 nodes and 64 nested levels.
Exhausting classification produces a safe internal error; exhausting a retry
lookup retains an already-selected status and omits `Retry-After`. Explicit
outer HTTP errors and reached retry metadata retain precedence. Custom error
methods still must return and support concurrent calls; the framework owns them
until they finish and contains panic/Goexit before writing a response. The same
bounds protect cursor-handler and custom asset-filesystem classification; an
unclassified filesystem failure does not establish a missing asset for SPA fallback.

The writer discards stale representation headers, preserves security/CORS/cookie
and method policy headers, and sends `application/json`, `nosniff` and `no-store`.
It does not serialize internal causes or models. Raw handlers still own protocol
policy such as `Allow` and authentication challenges.
[Application error declarations](http-application-errors.md) and
[validation](validation.md) share the endpoint response contract without
exposing internal causes.

The independent [consumer example](../../tests/fixtures/consumer/httpkernel/request_test.go)
uses public registration, typed attribution and `NotFound.WithCause` through real
TCP. Runtime and consumer race tests cover spoofed IDs, anonymous request scope,
bounded streams, early `100-continue` rejection, error identity, header preservation
and agreement between error metadata and actual responses.

Full repository verification also passed for this slice, including the real
PostgreSQL and gopls checks, compiler-rejection fixtures and generation freshness.
The [master acceptance record](../../blueprint/00-master-architecture-and-parity.md#http-request-limits-attribution-and-built-in-errors)
owns the detailed evidence. The HTTP blueprint records current milestone status.
