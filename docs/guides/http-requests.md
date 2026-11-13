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

The user agent is sanitized into the shared attribution bounds with
`attribution.SanitizeUserAgent`: invalid UTF-8 becomes U+FFFD, control characters
(including TAB and CR/LF) are removed and the text is truncated at 4,096 bytes on
a rune boundary. User-agent text never rejects a request, including health
checks. Outside an attributed operation, `RequestID` returns the empty ID. Background operations
can retain request provenance through the existing
[attribution contracts](model-references.md).

## Declared request header limits

Typed endpoints bind declared headers into their existing parameter type before
body decoding, preparation, authorization, resource binding and semantic
validation. Header cardinality, text and byte checks run before query/header
scalar codecs. Header conversion preserves decoded query fields; failed decoding
publishes no partially hydrated parameter value. Prohibited-input checks still
see supplied headers before preparation can change them.

`EndpointLimits.Headers` is a `RequestHeaderLimits` value configured with the
ordinary endpoint `Limits` option:

| Field | Default | Bound |
| --- | ---: | --- |
| `FieldBytes` | 8192 | Sum of raw value bytes for one declared name |
| `Bytes` | 16384 | Name plus raw value bytes per supplied declared field line |
| `Fields` | 32 | Declared names, checked at registration |
| `Values` | 64 | Supplied declared field lines before duplicate rejection |
| `Issues` | 16 | Returned framing diagnostics |

Bytes are charged before trimming spaces or tabs; splitting values across lines
does not evade a bound. Names alone must fit the registration budget. Bounds must
be positive, `FieldBytes <= Bytes`, `FieldBytes <= 8192` and `Fields <= Values`.
All-zero header limits are allowed only for endpoints without header declarations.
Undeclared headers are ignored. Trailers and `Request.Host` are not inputs here.

`RequestHeaderError` retains safe issues and an inspectable cause without exposing
raw values or codec messages. Endpoints return 400 with `RequiredIssue` for an
absent required header, `LengthIssue` for duplicates/size/count violations, and
`ValueIssue` for invalid text or scalar conversion. Field paths are escaped JSON
Pointers under `/headers`, using canonical names; aggregate limits use `/headers`.
`ValidateHeaders` supplies the same location for ordinary 422 semantic errors.

These are application-input budgets. `ServerConfig.MaxHeaderBytes` (32 KiB by
default) and the read-header timeout separately bound the entire network header
section, including cookies and undeclared fields. Native HTTP may reject that
section with 431 before routing. The typed encoder applies the endpoint budgets
as well; custom scalar formatters remain responsible for their own bounded work.

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

<a id="webhook-capture-in-the-request-lifecycle"></a>
## Webhook capture in the request lifecycle

A request under a prefix declared with `Builder.CaptureWebhooks` is selected by
its decoded path at a segment boundary, after the kernel assigns the request
ID and before maintenance, capacity and declared-length checks. If the
recorder admits it, the kernel snapshots method, escaped path, query, headers
and the trusted client address. Kernel rejections are stored without reading
the body.

For an admitted request the kernel reads at most the capture limit plus one
byte from the original stream, then replays those bytes followed by the rest of
the stream. Handlers, verifiers and route body limits see the original bytes,
`ContentLength` and the same terminal read error. The read happens before
routing, so a `100-continue` client gets `Continue` early and the read uses
the server read timeout rather than a route's extended deadline.

The completion snapshot is handed to the recorder before request observers
run. Status, outcome and accepted response bytes come from the kernel's
response observer; a raw panic is recorded as an aborted response with no
status. Persistence happens on one background writer, never on the request
path. See [webhook capture](webhooks.md#recipe-webhook-capture).

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
Exhausting classification produces a safe internal error; a panic or exhausted
retry lookup retains an already-selected status and omits `Retry-After`. Explicit
outer HTTP errors and reached retry metadata retain precedence, including the
one-second retry hint for a confirmed overload. `WriteError` contains panic and
`runtime.Goexit` from custom error methods before the response is committed. Methods must terminate and support concurrent
calls, and the framework retains ownership until they finish. The same
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

## Table operation declarations

`http.TableOperation(contract.TableOperationBinding{Table: tableID, Kind: contract.TableQuery})`
is an endpoint option; raw routes cannot carry it. Prefer the typed datatable
constructors and binders, which retain request, response, row, and actor types.
See [table operation bindings](datatable.md#declare-http-operation-bindings) for
query, ticket export, file export, and download declarations.
