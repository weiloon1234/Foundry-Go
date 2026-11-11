# Outbound HTTP clients

<a id="agent-entry"></a>
## Agent entry

- [Resolve a named client and bound a call](#recipe-named-http-client)
- [Send typed JSON](#recipe-http-request)
- [Read captured outbound calls](#recipe-outbound-capture)

## Overview

Milestone 24 delivered shared logical-operation observations and explicit
`Config.PropagateTrace`. Trace headers are off by default and remain subject to
the named client's header budget. See [observability](observability.md); the
milestone 24 verification passed as recorded in [production acceptance](../production-acceptance.md).

Milestone 20 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-20-verification-and-consumer-review)
records the checks and operational limits.

<a id="recipe-named-http-client"></a>
## Resolve a client and bound the request

Construct one named client per upstream and inject it into domain services:

<!-- compiled-source: tests/fixtures/consumer/background/account_client.go#named-client-deadline -->
```go
const AccountsClient httpclient.Name = "accounts.api"

type AccountService struct{ accounts *outgoing.Accounts }

func NewAccountService(services application.Services) (*AccountService, error) {
	client, err := services.HTTPClients.Client(AccountsClient)
	if err != nil {
		return nil, err
	}
	return &AccountService{accounts: outgoing.NewAccounts(client)}, nil
}

func (s *AccountService) Create(ctx context.Context, name string, token secret.String) (outgoing.AccountReceipt, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.accounts.Create(bounded, name, token)
}
```

Configure `accounts.api` under `services.http_clients.clients`; the explicit name never
falls back to a default. Resolve and retain the client during construction.
The child deadline also covers request encoding, response reads and decoding.
An error after provider submission can leave the external outcome unknown; keep
idempotency and reconciliation in the provider integration instead of retrying blindly.

For direct composition, `httpclient.New(config, nil)` selects an owned, reusable
standard-library transport. A supplied
`http.RoundTripper` is borrowed and is useful for adapters and deterministic
tests. It must honor context cancellation, close request bodies and avoid adding
its own retry/redirect policy. Construction performs no network I/O.

`httpclient.Module` binds a client to a typed foundation service key. Its
`requires` list identifies providers owning a borrowed transport. Shutdown cancels
operations and waits for callbacks, readers and actual close calls before those
dependencies are released. Direct callers use `Close(ctx)` and `Done()`; if Close's
waiting context expires, ownership remains visible through Done and Close can be
called again. A callback cannot wait for its own client shutdown.

<a id="recipe-http-request"></a>
## Requests and DTOs

<!-- compiled-source: tests/fixtures/consumer/outgoing/client.go#typed-http-request -->
```go
func (s *Accounts) Create(ctx context.Context, name string, token secret.String) (AccountReceipt, error) {
	request, err := httpclient.JSON(ctx, s.client.Post("accounts").Bearer(token), CreateAccountJSON(), CreateAccount{Name: name})
	if err != nil {
		return AccountReceipt{}, err
	}
	response, err := s.client.Do(ctx, request)
	if err != nil {
		return AccountReceipt{}, err
	}
	if err := response.EnsureSuccess(); err != nil {
		return AccountReceipt{}, err
	}
	return httpclient.DecodeJSON(ctx, response, AccountReceiptJSON())
}
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

`Form(url.Values)` sends an `application/x-www-form-urlencoded` replayable body.
`Multipart(httpclient.Field(name, value), httpclient.File(name, filename,
contentType, data)...)` encodes a replayable `multipart/form-data` body once,
within the request byte limit, and sets its boundary. `BasicAuth(user,
secret.String)` sets RFC 7617 credentials; the user ID cannot contain a colon.

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
500, 502, 503 and 504; a nonempty `RetryPolicy.Statuses` replaces that set (4xx/5xx
only). Transport failures before response consumption may retry;
body/consumer failures do not. Backoff starts at 100 ms and is capped at 2 seconds;
with `Jitter` (the default) each wait is drawn uniformly between zero and that
cap ("full jitter") so synchronized clients spread out. A `Retry-After` header
(seconds or HTTP date) on a retryable response sets the wait instead; a value
beyond `MaxBackoff` ends retrying and returns that response to the caller.
`NoRetries()` permits one attempt. `RetryPolicy.Mode = IdempotentOperation` is an
explicit assertion that a mutation is safe to repeat, typically using an upstream
idempotency key; supplying that header alone does not enable mutation retries.
Attempts are bounded to 1–10 and backoff to one minute.

The framework owns retries. Native requests keep `GetBody` unset. An empty body
is sent as `http.NoBody`, so the transport sends `Content-Length: 0` (never a
chunked empty body, which strict servers reject with 411) and starts no body
probe goroutine. A non-GET request carrying `Idempotency-Key` keeps an owned empty
reader instead, preventing standard-library idempotency-key POST replay from
bypassing this policy. A failed attempt closes its request and
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

`Download(ctx, request, destination, maximum)` streams a successful (2xx)
response into a borrowed `io.Writer` under its own byte limit (up to
`MaxDownloadBytes`), for files larger than `ResponseBytes`; other statuses fail as
`StatusFailed` without writing. A failed download can leave a partial write, so
write to a temporary file and publish it only on success. `DoAll(ctx, requests,
concurrency)` runs up to 4,096 requests with at most `concurrency` (no more than
`Concurrency`) in flight and returns outcomes in request order; each request keeps
its own admission, deadlines and retry policy.

Defaults are 64 concurrent operations, 10 seconds to connect, 30 seconds per
attempt, one minute per complete operation, 4 MiB request and response bodies,
and 64 KiB response/request header metadata. Configuration can reduce these or
increase them within explicit caps: 4,096 operations, one-hour deadlines, 64 MiB
bodies and 1 MiB headers. A caller context can impose a shorter deadline. Response
limits apply to decompressed bytes. Header entry/value counts are also bounded.
Operations beyond `Concurrency` wait in FIFO order for at most `min(Timeout, 5s)`
and then fail as a `*httpclient.Error` of kind `Overloaded` (it also matches
`fault.Overloaded`); no request was sent.

`Config.TLS` customizes the owned transport: `CertificateAuthorities` (PEM)
replaces the system roots unless `AppendSystemRoots` is set, `Certificate` and
`PrivateKey` (PEM secrets, set together) present a client certificate, and
`ServerName` overrides verification of the URL host. TLS options cannot combine
with a caller-supplied transport. `Module` snapshots header, destination and
retry-status slices when it is called.

`Snapshot()` reports admitted requests, transport attempts and failures without
retaining input data. Errors expose stable kinds, client name, method, attempt and
status. Normal diagnostics omit URLs, query values, headers, bodies and underlying
error text. Internal causes remain available through explicit error inspection.

## Tests

`testkit/httpclient.New` takes `Respond(status, headers, bytes)` or `Fail(err)`
outcomes and implements `http.RoundTripper`. It owns copied outcomes and recorded
requests, never falls through to the network and reports exhaustion explicitly.
`Requests`, `Sent` and `Pending` inspect behavior; request accessors explicitly
reveal test data while formatting stays redacted. A request is recorded only
after its complete body was read.

`NewRoutes(On(method, pattern, outcomes...)...)` answers each request from the
first matching route instead of arrival order. Patterns use `path.Match` syntax
against `host/path` (for example `api.example.com/v1/users/*`); an empty method
matches any method. An unmatched request fails with `Unmatched`, an exhausted
route with `Exhausted`. `AssertSent(t, match)`, `AssertNotSent` and
`AssertSentCount(t, n, match)` check recorded requests with a predicate.

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

`client.RestrictsDestinations()` reports whether a client enforces such a policy;
framework features that fetch untrusted URLs, such as attachment
`AddFromURL`, require it.

Denied requests match `httpclient.DestinationDenied` through `errors.Is`, including
wrapped transport failures. A denied URL is rejected before its body factory runs;
DNS-policy rejection is not retried. Never attach upstream credentials to a
general arbitrary-URL client.

<a id="recipe-outbound-capture"></a>
## Outbound capture

Inside an application with a database, every attempt a client built by
`httpclient.Module` makes is stored in `foundry_outbound_captures`, best
effort and off the hot path, so developers can see what the application sent
and what came back. No call site changes: settings-named clients
(`services.http_clients`), modules an application registers itself and the
framework features that borrow them (`webhook/outbound`, attachment imports,
`auth/oauth`) are all covered. Capture never adds a read, changes a byte,
replays a body or changes retry eligibility, and it never fails a call.

A row is one wire attempt. Its `operation_id` groups the attempts of one `Do`,
`Stream` or `Download`; `retried` marks an attempt the client followed with
another. A call that never reached the wire (the client's admission was full,
or the destination policy refused the URL) is one row with attempt `0`. Each
row stores the time and duration (plus time to response headers), the client,
method, scheme, host, path and query, the final request headers after defaults
and trace propagation, the response status, protocol and headers, both bodies
with how much was sent or read and whether the end was observed, the remote
address and whether the connection was reused, one closed `result`, and the
request ID, trace ID and route of the inbound request that made the call.

| Path | Captured | Why |
| --- | --- | --- |
| `httpclient.Module` clients in an application with a database | yes | All application calls |
| Framework features borrowing those clients | yes | They run on application clients |
| `httpclient.New` outside assembly, tests | no | No application, no store |
| Email providers, `storage/s3`, OTLP export, cloud credentials | no | Framework infrastructure with its own transports |
| Applications without a database | no | Nothing to store into; `Services.OutboundCaptures()` reports missing |

Mount the viewer inside an existing guarded group with the store from
`services.OutboundCaptures()`, behind the same developer gate as the webhook
viewer. At least one access option is required and the routes send
`Cache-Control: no-store`.

<!-- compiled-source: tests/fixtures/consumer/teamworkflow/outbound_capture.go#outbound-capture-routes -->
```go
func OutboundCaptureRoutes(captures *outboundcapture.Store) ([]http.RouteRegistration, error) {
	transport, guard, err := authfixture.Authentication(ReadCaptures.Registration())
	if err != nil {
		return nil, err
	}
	binding, err := http.BindGuard(transport, guard)
	if err != nil {
		return nil, err
	}
	return outboundcapture.Routes(http.DefineScope("/developer", "developer"), captures, binding, http.Permissions(ReadCaptures))
}
```

The group's IDs become `<group>.outbound-captures.list` and `.show`, generated
client operations such as `developerOutboundCapturesList`. The list takes
`from` (required), `until`, `client`, `host`, `method`, `status`, `result`,
`problems` (not completed, or status 400 and above), `request_id` and
`operation`, spans at most 30 days and pages newest first with 30 items by
default and 100 at most. The detail adds headers, query values, both bodies
(base64), connection facts, the cause and the operation's other attempts.

### Results

| `result` | Meaning |
| --- | --- |
| `completed` | Response headers arrived and the attempt ended without a failure, at any status; a retried `503` and a `Download` of a `404` are completed |
| `consumer_failed` | The `Stream` callback or `Download` destination failed |
| `response_body_failed` | Reading the response body failed |
| `response_too_large` | `Content-Length` or the body exceeded the client's limit |
| `request_body_failed` | Opening or reading the request body failed |
| `transport_failed` | Connect, TLS, protocol or response header failure |
| `timed_out` | The attempt or operation deadline passed |
| `canceled` | The caller's context was canceled, including a client closed at shutdown |
| `destination_denied` | The destination policy refused the URL (attempt `0`) or the dialed address |
| `overloaded` | The client's admission was full; nothing was sent (attempt `0`) |

A body is complete only when its end was observed, never because a prefix
parsed. `Download` does not read a non-2xx body, so that body is stored as
`incomplete`; a `Stream` callback that stops early leaves the rest unread too.
An in-memory request body (`Bytes`, `Form`, `JSON`, `Multipart`) is stored as
offered, and `request_body_complete` says whether the transport sent all of
it: a refused connection stores the body with `request_sent_bytes` `0` and
`request_body_complete` false.

### Privacy

Redacted mode is the default for every client. Credential names are masked in
request headers, response headers, query values and JSON, form and multipart
fields in every mode, with one rule shared with webhook capture: the audit
sensitive-name rule (`Authorization`, `Cookie`, `Set-Cookie`, `X-Api-Key`,
`client_secret`, `access_token` and similar), the word `auth` (`auth`,
`X-Auth-Token`, `authToken`; `author` and `oauth_state` stay visible), a final
word `key` after `-`, `_` or `.` (`X-RapidAPI-Key`, `?key=`;
`Idempotency-Key` and words such as `monkey` stay visible), the joined names
`apikey`, `accesskey`, `secretkey`, `privatekey`, `subscriptionkey` and
`signingkey`, and in query strings the framework's `ticket` and `signature`
parameters and the words `sig` and `signature` (`X-Amz-Signature`). The same
query rule masks values in the query or fragment of URL-valued headers in both
directions (`Location`, `Content-Location`, `Referer`, `Link`, `Refresh`), so a
presigned redirect or a signed next-page link is not stored replayable.
Signature headers such as `Stripe-Signature` stay visible. Redacted mode
replaces long values with their size, keeps multipart file names, media types
and sizes but never file bytes, masks multipart value parts without a form
name, and stores only JSON, URL-encoded and multipart bodies; compressed and
other media types, such as HTML pages and downloaded files, are counted but
never buffered.

Two Go declarations change body disclosure; deployment settings can only
reduce it. `Builder.FullOutboundCapture(names...)` stores the complete bodies
of those clients exactly within `config.body_bytes` (a longer or unfinished
body becomes an exact prefix); credential headers and query values stay
masked, `config.force_redacted` turns it off, and naming a client the
application never constructs fails `Build`. `Request.WithoutBodyCapture()`
keeps the call's row but stores both bodies as `declined`, for payloads that
are secret by meaning rather than by field name:

<!-- compiled-source: tests/fixtures/consumer/teamworkflow/outbound_capture.go#outbound-capture-decline -->
```go
func SendCode(ctx context.Context, sms *httpclient.Client, to, code string) error {
	request := sms.Post("/messages").Form(url.Values{"To": {to}, "Body": {code}}).WithoutBodyCapture()
	response, err := sms.Do(ctx, request)
	if err != nil {
		return err
	}
	return response.EnsureSuccess()
}
```

Paths are never masked: credentials inside URL paths, such as a bot token or a
webhook URL secret, are stored, in the call line and in URL-valued headers
alike; exclude those hosts. Add vendor credential names the rule misses with
`config.mask_names`.

### Settings

Runtime settings live under `features.outbound_capture`.

| Key | Default | Meaning |
| --- | --- | --- |
| `enabled` | `true` | `false` stops capturing; the viewer and retention keep working |
| `database`, `schema` | default connection, its schema or `public` | where the table lives; an explicit unknown database fails `Build` |
| `config.force_redacted` | `false` | store every client redacted |
| `config.mask_names` | `[]` | extra credential names, exact and case-insensitive (at most 64) |
| `config.exclude` | `[]` | deny-list rules (at most 64), see below |
| `config.body_bytes` | 1 MiB (1 KiB to 8 MiB) | stored bytes per body |
| `config.value_bytes` | 4 KiB (64 B to 1 MiB) | longer redacted values read `[omitted N bytes]` |
| `config.rate`, `config.burst` | 20, 40 (1 to 10,000) | captures admitted per second per client per process |
| `config.queue_rows` | 256 (1 to 256) | captures in flight, queued or being written per process; one client holds at most a quarter |
| `config.retained_bytes` | 64 MiB (up to 1 GiB) | memory those captures hold; one client holds at most a quarter, which must fit one body |
| `config.retention` | 30 days (1 hour to 365 days) | age after which rows are deleted and hidden |
| `config.max_rows` | 20,000 (1,000 to 10,000,000) | newest rows kept per client |

`config.exclude` is a deny-list. A rule matches when every field it sets
matches, and a matching call is not captured; rules see only the request.

```toml
[[features.outbound_capture.config.exclude]]
client = "crawler"                 # a rule with only a client excludes it entirely

[[features.outbound_capture.config.exclude]]
host = "*.ingest.sentry.io"        # exact host, or "*." for any subdomain (not the apex)

[[features.outbound_capture.config.exclude]]
client = "visibility"
method = "GET"
path_prefix = "/api/v1/models"     # decoded path, segment by segment
```

An empty rule, a duplicate, a host with a scheme, port or path, a bare `*`, a
lower-case method or an invalid prefix fails `Build`. A client-only rule naming
a client the application does not construct is logged once at `Build`, so one
settings file serves environments whose conditional clients differ.

### Loss, retention and operations

Each client has its own admission: a token bucket, at most a quarter of the
process queue and memory, and its own loss counter, so a crawler burst cannot
crowd out a payment gateway's rows. Admission refusals, a full queue, failed
writes (after two retries), a missing table and shutdown leftovers are
counted; the client's next stored row carries the count in `dropped_before`,
and the process logs `outbound captures were not stored` at most once a minute
with counts by reason. A body the memory budget refuses is stored as
`capture_memory` without losing the row. A crash loses queued captures.

Every process writes, because every process constructs its clients; a process
checks the table on its first capture, so a command that makes no call never
queries it. Retention runs beside every kernel except CLI: rows older than
`config.retention` are deleted, then each client keeps its newest
`config.max_rows`, in bounded batches under an advisory lock. At shutdown
clients drain first and the store then writes what remains within 3 seconds.

Apply the framework migration
`foundry.outbound_capture/000001_create_outbound_captures` with the ordinary
migration command; it is registered whenever the application has a database,
also while capture is disabled. Register `application.OutboundCaptureCommand()`
for `outbound-captures prune`, which counts rows past retention and the
per-client cap, and removes them with `--apply`.

