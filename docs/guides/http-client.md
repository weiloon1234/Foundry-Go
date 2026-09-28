# Outbound HTTP clients

Milestone 24 delivered shared logical-operation observations and explicit
`Config.PropagateTrace`. Trace headers are off by default and remain subject to
the named client's header budget. See [observability](observability.md); the
milestone 24 verification passed as recorded in [production acceptance](../production-acceptance.md).

Milestone 20 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-20-verification-and-consumer-review)
records the checks and operational limits.

Construct one named client per upstream and inject it into domain services:

```go
config := httpclient.DefaultConfig("accounts.api")
config.BaseURL = "https://accounts.example.com/v1"
client, err := httpclient.New(config, nil)
```

`nil` selects an owned, reusable standard-library transport. A supplied
`http.RoundTripper` is borrowed and is useful for adapters and deterministic
tests. It must honor context cancellation, close request bodies and avoid adding
its own retry/redirect policy. Construction performs no network I/O.

`httpclient.Module` binds a client to a typed foundation service key. Its
`requires` list identifies providers owning a borrowed transport. Shutdown cancels
operations and waits for callbacks, readers and actual close calls before those
dependencies are released. Direct callers use `Close(ctx)` and `Done()`; if Close's
waiting context expires, ownership remains visible through Done and Close can be
called again. A callback cannot wait for its own client shutdown.

## Requests and DTOs

```go
request, err := httpclient.JSON(ctx,
    client.Post("accounts").Bearer(token),
    CreateAccountJSON(), CreateAccount{Name: "Ada"},
)
response, err := client.Do(ctx, request)
if err != nil { return err }
if err := response.EnsureSuccess(); err != nil { return err }
receipt, err := httpclient.DecodeJSON(ctx, response, AccountReceiptJSON())
```

Generated `contract.JSON[T]` descriptors own both request encoding and response
decoding. Wrong payload/descriptor types fail compilation; wire-shape mismatches,
duplicate fields, unexpected fields and bounds fail decoding. Exact large integer
and decimal representations follow the DTO contract. JSON encoding occurs once
before sending and uses the request byte limit.

`Request(method, path)`, `Get` and `Post` construct immutable client-bound values.
`Header`, `Bearer(secret.String)`, `QueryPair`, `WithBody` and `WithRetry` return
copies. Invalid builders retain an error and fail before opening a body or invoking
a transport. Requests cannot cross client instances. `URL()` explicitly exposes the
complete URL; ordinary formatting omits it and all header/body values.

A configured base URL accepts relative request paths beneath its prefix. Absolute
or network-path references, dot-segment escapes, URL user information and fragments
are rejected; default credentials cannot silently move to another origin.
Without a base URL, requests require an absolute HTTP(S) URL. Redirects are returned
as responses and never followed. Cookies are explicit headers; there is no hidden
cookie jar.

## Body ownership and retries

`Bytes(data)` copies a bounded body and makes it replayable. `StreamBody(length,
opener)` opens a single-use stream; `-1` means unknown length. `ReplayableBody`
explicitly asserts that each call opens an independent reader with identical
content. Openers transfer reader ownership to the client, and readers must support
concurrent Read/Close so cancellation can interrupt them. Returned readers are
closed even when an opener also returns an error. Known lengths and byte bounds
are enforced; failed reads cannot be hidden by a custom transport.

The default retry policy makes at most three total framework transport attempts
for GET, HEAD and OPTIONS, with replayable bodies. Retryable statuses are 408, 429,
500, 502, 503 and 504. Transport failures before response consumption may retry;
body/consumer failures do not. Backoff starts at 100 ms and is capped at 2 seconds.
`NoRetries()` permits one attempt. `RetryPolicy.Mode = IdempotentOperation` is an
explicit assertion that a mutation is safe to repeat, typically using an upstream
idempotency key; supplying that header alone does not enable mutation retries.
Attempts are bounded to 1–10 and backoff to one minute.

The framework owns retries. Native requests keep `GetBody` unset and retain an
owned reader even for empty bodies, preventing standard-library idempotency-key
POST replay from bypassing this policy. A failed attempt closes its request and
response streams before another opener runs. Small rejection bodies are drained
up to 64 KiB for connection reuse; larger ones are closed. The standard transport
can still perform connection establishment/protocol recovery that sends no
application request; attempt counts describe RoundTrip invocations.

## Responses and streaming

`Do` returns only a complete body. `Response.Bytes()` and `Headers()` return copies;
`Text()` requires valid UTF-8. `EnsureSuccess()` accepts 2xx statuses. Other statuses
remain available for explicit application handling, including redirects.

```go
err := client.Stream(ctx, client.Get("accounts/export"),
    func(ctx context.Context, response *httpclient.StreamResponse) error {
        if err := response.EnsureSuccess(); err != nil { return err }
        _, err := io.Copy(destination, response)
        return err
    },
)
```

The callback receives the attempt deadline and operation ownership context. Once
it starts, there are no retries. Its reader is closed on success, failure, panic,
Goexit or cancellation, and retained readers reject later reads. Admission remains
occupied until the callback and underlying reads/close calls actually return.
An uncooperative borrowed transport cannot be forcibly terminated; the client
continues to account for its lifetime.

Defaults are 64 concurrent operations, 10 seconds to connect, 30 seconds per
attempt, one minute per complete operation, 4 MiB request and response bodies,
and 64 KiB response/request header metadata. Configuration can reduce these or
increase them within explicit caps: 4,096 operations, one-hour deadlines, 64 MiB
bodies and 1 MiB headers. A caller context can impose a shorter deadline. Response
limits apply to decompressed bytes. Header entry/value counts are also bounded.

`Snapshot()` reports admitted requests, transport attempts and failures without
retaining input data. Errors expose stable kinds, client name, method, attempt and
status. Normal diagnostics omit URLs, query values, headers, bodies and underlying
error text. Internal causes remain available through explicit error inspection.

## Tests

`testkit/httpclient.New` takes `Respond(status, headers, bytes)` or `Fail(err)`
outcomes and implements `http.RoundTripper`. It owns copied outcomes and recorded
requests, never falls through to the network and reports exhaustion explicitly.
`Requests`, `Sent` and `Pending` inspect behavior; request accessors explicitly
reveal test data while formatting stays redacted.

The fake caps requests/outcomes at 1,024, a body at 1 MiB and retained request or
outcome data at 16 MiB, URLs at 16 KiB, and header metadata at 64 KiB. Runtime clients use their own independent limits.

The [independent outgoing consumer](../../tests/fixtures/consumer/outgoing/client.go)
demonstrates a thin upstream service, generated DTOs, exact IDs and callback-scoped
downloads. Email provider adapters share the native transport constructor while
retaining their existing single-attempt and conservative delivery classification.

## Restricting destinations from untrusted input

Use the ordinary client for explicitly configured internal services. For URL
previews/imports or other untrusted URLs, opt into an enforced destination policy:

```go
settings := httpclient.DefaultConfig("preview")
settings.Destination = httpclient.PublicDestinations() // HTTPS, port 443
settings.Destination.Hosts = []string{"images.example.com"} // optional exact names
client, err := httpclient.New(settings, nil)
```

`Destination` is also part of configured named HTTP-client settings. The policy
is snapshotted at construction. Schemes and ports are explicit allowlists. An
empty host list allows any valid host; an empty network list allows only public
Internet addresses. Nonempty `Networks` is an exclusive CIDR allowlist: explicitly
add approved private service networks with `netip.MustParsePrefix` when needed.
Use canonical prefixes and ASCII/punycode hostnames; wildcards and zone IDs are
not accepted.

Restricted clients connect directly, bypass environment proxies, and reject custom
transports. Each new connection resolves the destination, checks every returned
IPv4/IPv6 address, and dials an approved IP literal. A mixed public/private DNS
answer fails closed; a later DNS change is checked on the next connection. Pooled
connections retain their already-checked peer. Redirects remain unfollowed. TLS
continues to verify the original hostname. The public-only policy also excludes Azure's
[platform WireServer address](https://learn.microsoft.com/en-us/azure/virtual-network/what-is-ip-address-168-63-129-16),
which uses a globally unicast address. Network routing/NAT and the service itself
remain deployment trust boundaries.

Denied requests match `httpclient.DestinationDenied` through `errors.Is`, including
wrapped transport failures. A denied URL is rejected before its body factory runs;
DNS-policy rejection is not retried. Never attach upstream credentials to a
general arbitrary-URL client.
