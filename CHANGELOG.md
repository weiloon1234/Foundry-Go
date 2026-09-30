# Changelog

## Unreleased

### SPA fallbacks with application.New

- `application.Builder.SPA(id, assetsKey, config)` declares a browser portal's
  client-route fallback on the application router, using the existing
  `http.SPAConfig`; several portals can use distinct prefixes. Build rejects an
  unknown assets key, an invalid configuration and a route ID or prefix that
  repeats another SPA or a declared route before any asset directory opens.
  Route inspection lists each SPA; contract export is unchanged.
- `http.RegisterSPA` adds the same declaration to a router assembled from
  contributions; `Builder.SPA` uses it.
- SPAs now compose with asset mounts by prefix. A SPA more specific than a
  matching mount owns its subtree, so a root `public/` mount no longer answers
  (and 404s) client routes under `/admin`; a SPA with the same prefix answers
  the mount's misses, so a portal at `/` can sit beside a root `public/` mount.
  Declared routes and more specific mounts, such as immutable hashed bundles,
  keep precedence. See [SPA fallbacks with application.New](docs/guides/http-assets.md#spa-fallbacks-with-applicationnew).

### Re-audit of client descriptors, forms and startup diagnostics

- Form submissions report actual outcomes. Edits and reset no longer abort a sent
  request; its `succeeded`/`failed` result is returned with `changed`, and only
  `cancel()`, `dispose()` or the caller's signal abort, returning `canceled` with
  `outcome` `not_sent` or `unknown`. `stale` is no longer produced. Non-`AbortSignal`
  signals are rejected before the busy guard is taken.
- Form tasks release capacity at once when a run is replaced or invalidated before
  its debounce ends, so re-running several tasks from one handler is not refused.
  Abort listeners observe the edit that aborted them. Built-in number/boolean
  parsing is exact: surrounding whitespace, `+` and the text `null` fail.
- The Vue adapter subscribes when its component mounts, so server rendering leaves
  no subscription on a borrowed store.
- Descriptors navigate maps keyed by model IDs through `.at(identity)`; `.at`
  applies the codec's key rules and fixed array lengths; enum choices decode once
  per type; the descriptor document checks the manifest version. TypeScript's
  `PresentationKind` is generated from the new `contract.PresentationKinds()`.
- Password hints stay out of outputs and URLs: route registration rejects JSON and
  event-stream responses reaching one, even without a client export; path, query
  and realtime room parameters reject them. `contract.PasswordTypes` is the shared
  reachability owner.
- `contract.MaxPresentationKeyBytes` is now 128, the message-key limit keys already
  had to meet. Contradictory hints on enum fields and plain scalar transport fields
  fail generation with a source diagnostic; codec contradictions found at
  registration name their type and field. Manifest label/format consistency is
  checked per element for repeated inputs and for a comparison's other field, and
  OpenAPI places a repeated input's hint on its items. `validation.EmailRuleID` and
  `validation.URLRuleID` own the rule IDs presentation checks recognize.
  See the [re-audit evidence](docs/evidence/client-reaudit-20260930.json).
- Form task runs ended by a newer run, an edit, `cancel()` or disposal resolve
  `canceled` whether or not their callback had started; `stale` is no longer a
  task result. `variant()` accepts only the key that tags every union member, so
  an enum-typed property of a plain object no longer type-checks as one.
  Contradictory hints on multipart file and JSON parts name the part. The form
  guide documents a React `StrictMode`-safe owner that creates the controller in
  the effect that disposes it.

### Form controllers and startup diagnostics

- Generated `createForm` owns typed drafts, explicit exact parsing, touched/dirty
  state, existing validation reports, server issue pointers and guarded submission.
  Bounded async tasks prevent stale options/check results replacing current input.
- Optional `--react`/`--vue` exports subscribe to the same core state. Their modules
  are separate; the pure core SDK imports no UI dependencies. See the
  [form guide](docs/guides/client-forms.md).
- Database Modules log startup/ready, transient retries and terminal failures with
  safe pool/attempt/timing/classification metadata. Direct pools can use
  `WithStartupLog`. Retry behavior and timeouts remain unchanged; B08 stays open.

### Typed client descriptors and presentation

- Generated `operation(name)` and `schema(typeID)` expose typed, immutable field
  descriptors, nested/collection/union navigation, exact enum choices and route
  metadata. Calls and validation reuse the existing SDK owners.
- Optional `client` field tags and typed `contract.Presentation` values supply
  semantic hints and shared label/help keys across JSON, URL and multipart
  declarations, manifest and OpenAPI. Contradictory/bounded metadata is validated;
  explicit password hints reject public output graphs and credential examples.
- Manifest format is now 6. Regenerate saved manifests, OpenAPI and TypeScript
  together with the matching runtime/tool. See the
  [descriptor guide](docs/guides/client-descriptors.md).

### Model extension slots

- E01: models declare `translations.Text`, `attachments.One`/`Many` and
  `metadata.Value` fields with an optional `DefineExtensions` policy method.
  Generation emits the policy set, typed slot descriptors, the extension owner,
  `From(slots.Runtime)` binding and a declaration per model, plus a package-level
  `FoundryExtensions()`. `application.Builder.Models` registers owners and slots
  with the existing managers and a hard-delete cleanup observer on the extension
  database; `Services.ModelExtensions()` returns the enabled managers. Slot stored
  names follow the Go field (`foundry:"name=..."` pins one) and the
  `extension_owner=` directive pins the owner. `attachments.Attachment` now embeds
  the owner-free `attachments.File`; its method set is unchanged.
  `translations.Options` gains `Require`, an `i18n.LocaleRequirement`, and
  `contract.JSON` gains `IsZero`.
  See the [slot guide](docs/guides/model-extension-slots.md).
- E02: bound slot descriptors are eager-loading relations. `With`, `Load`,
  `LoadMissing`, pagination, chunked iteration and nested relations fill
  slots with a constant number of statements per parent batch; batches beyond
  a store's row or byte limit are halved and retried. A load joins the caller's
  transaction on the extension store's pool and otherwise reads the store's own
  snapshot. Slots expose pure reads (`Exact`, `Resolve`, `ResolveRequest`,
  `Get`, `Len`, `All`) and attachment descriptors derive public, temporary and
  variant links from loaded files. `query.ExtensionSlot`, `extensions.LoadInParts`
  and `Store.ReadFor` are the new extension boundaries.
- E03: bound descriptors write for a model value. Translated slots offer
  `SaveIn` (merge) and `SyncIn` (exact, enforcing `Require`) plus forget and
  clear; metadata slots offer `SaveIn` and `ForgetIn`; attachment slots publish
  any `attachments.FileSource`, such as `foundryhttp.UploadedFile`, through
  `ReplaceFile`/`AddFile` and `attachments.AddFiles`, and check uploads with
  `Accepts` using write-time detection. `TextSlot.Rule()` (complete input) and
  `MergeRule()` (partial input) validate locale-keyed input through the new
  server-only `validation.Locales` and `validation.MaxBytes` rules (issue codes
  `foundry.supported_locale` and `foundry.max_bytes`).
- E04: `inspection.Sources.Models` adds an `extensions` section describing
  owners and slots (field, kind, stored name, storage and policy bounds) from
  declarations alone. Read-only `translations`, `metadata` and `attachments`
  `undeclared` commands and `InspectUndeclared` functions list stored names no
  registration declares, for example after renaming a slot field. The
  independent `articles` consumer exercises slots over real HTTP, PostgreSQL and
  local storage. `App.Models()` returns the declarations registered with
  `Builder.Models` for inspection.
- E05 re-audit: slot loading charges the relation budget per parent batch and
  enforces `MaxDepth`; `validation.Locales` reports unrepresentable locale keys
  at the map instead of failing execution; translated input rules reject NUL
  text and `SyncIn` shares the rule's blank check; `Accepts` buffers under the
  attachment write admission; projections reject `foundry:"name=..."`;
  generation refuses scalar inference for enum metadata, reports malformed
  `JSONContract` methods and duplicate owner names, and field notices state
  whether a stored name is pinned. See the [blueprint series](blueprint/model-extension-slots/README.md)
  and the [E05 acceptance evidence](docs/evidence/model-extension-slots-e05.json).
- Extension batch reads check each owner's identity once instead of re-deriving
  it for every stored row: a row whose stored identity is its owner's canonical
  identity is matched directly, and any other row, such as one recorded before a
  declared table rename, is still validated in full. `extensions.Owner.SubjectKey`
  and `Registry.SubjectKey` return `Subject.Key` without its identity snapshot,
  and `Owner.Active` now returns the active subject keys as `map[string]bool`,
  like `RetainedSubjects`, instead of full subjects. Loading two translated
  fields and one metadata value for 1000 articles fell from about 281 ms and
  180 MB to 166 ms and 86 MB ([measurements](docs/evidence/model-extension-slots-performance-20260930.json)).

### Stabilization review

- Rotating log files no longer drop a newer retention request while an older one
  is queued. The pending request now keeps the latest time, so an archive that
  expires is removed on the write that expires it rather than up to one prune
  interval later under load.
- Query row cancellation hooks are registered under the row mutex. Cancellation
  during driver return can no longer race cleanup-hook initialization; forgotten
  streams still release their connection and owner automatically.
- Cache `Stats.Loads` counts actual loader invocations, including failed loaders;
  a fill whose storage recheck finds fresh data no longer increments it. This
  fixes timing-dependent overcounting during stale-while-revalidate reads.
- Outbox `PublishOne`, `PublishBatch` and `Run` reject nil and uninitialized
  publishers with `fault.Invalid` instead of dereferencing the receiver first.
- Transaction retry now inspects the complete bounded error graph inside callback
  isolation. Committed, unknown and unconfirmed outcomes veto replay; custom
  error panics, Goexit and cyclic wrapping cannot escape or trigger retries.
  Exponential delays saturate without duration overflow and cancellation stops
  another attempt.
- Policy, permission, hook and guest-policy denial inspection stays inside the
  authentication scope's owned callback. Wrapped typed denials remain supported;
  inspection failures cannot grant access or release scope ownership prematurely.
- Impersonation resume checks the unchanged live actor session and rechecks
  expiry before commit in one transaction. Concurrent resume, revocation and
  rotation cannot recreate a credential from a stale listing. (The second review
  later changed resume to rotate the actor session in place through
  `session.ResumptionBackend.ResumeActor`.) PostgreSQL implements the optional
  `session.ResumptionBackend`; custom adapters must implement it before
  supporting resume. `session.DeadlineBackend` shares checked
  creation deadlines with MFA completion.
- OAuth/OIDC rejects trailing JSON data and enforces the documented minimum RSA
  signing-key strength of 2048 bits.
- The native idempotent-client fixture now receives the manifest version from
  its Go owner instead of retaining a stale version-4 assertion. Publisher
  shutdown has deterministic coverage for driver failures that do not wrap the
  task's cancellation error, while one-shot publication preserves the failure.

Acceptance scope and rollout handoff: [stabilization review](docs/guides/stabilization-20260929.md).

### Second independent review

After the stabilization pass, eight read-only reviewers re-examined all changed
areas and the owning implementers fixed about 110 confirmed defects with
regression tests. The most important corrections: impersonation can no longer
mint unmarked credentials, extend the actor's absolute lifetime or survive the
actor's revocation; invalid polymorphic names can no longer drop the morph type
filter; HMAC webhook presets derive delivery IDs from signed content;
S3-compatible disks require explicit credentials; maximum-size Redis workflows
with callbacks and migrated queues can no longer corrupt a queue; global
compression, ETag and browser-session middleware honor per-route timeouts;
WebSocket membership conflicts and mixed-version relays no longer stop a hub;
maintenance allowlists resolve the trusted-proxy client and bypass cookies use
application keys; cache stores drain background loads at shutdown; and lease
tokens are single-use. Each fix is listed under its area's "Fixed" section below.
Record: [second review](docs/guides/second-review-20260929.md).
Subsequent package, starter, PostgreSQL, security and fuzz evidence, including an
unresolved Linux queue stress timeout, is in the
[acceptance follow-up](docs/guides/second-review-acceptance-20260929.md).

### Framework improvement program

A framework-wide audit against Laravel-style production expectations was
followed by fixes for every confirmed defect, performance work backed by
focused benchmarks and additional parity features. A second independent review
of every area then fixed further defects, which are included in the sections
below. Upgrade notes for persisted formats and rolling deploys are in
[compatibility](docs/compatibility.md). The sections below are grouped by
area; earlier unreleased changes follow them.


### Failure diagnostics, overload and shared runtime

- Server failures are now diagnosable. Every 5xx response is logged once as
  `HTTP request failed` with its route, status, error code and a redacted
  `fault.Diagnostic`: Go type names, framework fault codes and messages,
  framework attributes such as SQLSTATE, database code and constraint, and the
  source frames of contained panics. Error text, payloads, credentials and panic
  values are never formatted. `observability.ErrorReport.Diagnostic` carries the
  same summary to reporters, and `Span.EndWithDiagnostic` lets kernels attach it.
- Contained panics keep up to 32 source frames (`(*fault.Error).Frames()`), never
  the recovered value.
- The new `fault.Overloaded` classifies temporarily exhausted capacity. HTTP maps
  it to `503 unavailable` with `Retry-After: 1` instead of an internal error.
  Unclassified deadlines and cancellations (the server's own request budget) now
  map to `503 unavailable` instead of `408 request_timeout`.
- Bounded operation scopes and credential gates now queue bursts in FIFO order
  for a short bounded wait before reporting `fault.Overloaded`, instead of
  rejecting immediately with `fault.Conflict`. Nested admission into the same
  scope never waits. A credential callback that completed is reported as
  successful even when its deadline expires afterwards.
- Redis Lua scripts run with `EVALSHA` and upload their source only after a
  `NOSCRIPT` reply, which proves the script did not run. This removes the full
  script body from every cache, lease, rate-limit, job and WebSocket command.
- Credential-bearing responses (token, MFA enrollment/recovery and browser-session
  cookies) are never published after the request context ended, even when the
  issuing handler completed; the client obtains a fresh credential. Ordinary
  successful responses are still delivered after a late deadline.

### HTTP core pipeline and contracts

#### Fixed

- Global `Compression`, `ETags` and `BrowserSessions` no longer cut routes that
  declare a longer `WithTimeout` at the kernel `RequestTimeout`: they observe the
  matched route's effective context while it runs, and after a typed handler
  succeeded only client disconnect or forced shutdown. Event streams, progressive
  streams, long downloads and session logins on such routes now complete, and a
  success or error response completed after its deadline is delivered through
  these wrappers.
- `BrowserSessions` no longer turns every response completed after its deadline
  into a 503: only a publication that carries a staged session cookie (login,
  rotation or logout) is withheld once the request context ended.
- Download and stream sources opened after an expired deadline detach only from
  that deadline; client disconnect and forced shutdown still end them, and a
  client that already left prevents the source from opening. Previously such a
  transfer ignored all cancellation and could hold server shutdown.
- A typed event stream whose handler completed after its deadline now returns a
  reported 503 instead of an empty 200. `ServeEvents` defers its sink shutdown and
  producer wait, so a writer that exits abnormally (for example `runtime.Goexit`
  in an event codec) never releases the request while the producer runs.
- Admission rejections (capacity exhaustion, a closing server, draining and
  maintenance 503s) are no longer described and logged at ERROR per request;
  they remain counted as rejected outcomes. Real server failures are still
  reported with redacted diagnostics.
- The `LocaleWith` query selector (for example `?lang=ms`) no longer makes typed
  endpoints reject the request as an undeclared parameter, and no longer
  invalidates a signed link it is appended to. An endpoint that declares the same
  parameter still receives it.
- `Forwarded` parsing splits elements at every comma, so a client's unterminated
  quote can no longer swallow the hop a trusted proxy appended to the same line
  and attribute the client to the proxy's address (lockout and rate-limit bucket
  poisoning).
- `SignedURLInfo` now exports each signed route's link policy (`relative`,
  `permanent`, `ignored_parameters`) to route inspection, the manifest and
  OpenAPI, and the TypeScript client accepts relative, permanent and decorated
  signed links according to it.

- Every typed-endpoint 5xx, including codec, multipart, validation-infrastructure
  and response-encoding failures, reaches `WriteError`'s redacted server-failure
  report. A response write that fails after commit records its diagnostic on the
  request observation before aborting.
- The router matches each request once. Native canonical redirects still pass
  through unchanged, including headers set by earlier middleware.

#### Changed

- The client contract manifest is now version 5 (alternative statuses, redirect
  responses, raw request bodies and event stream responses). Regenerate saved
  manifests, OpenAPI and TypeScript clients together; older readers reject the
  new version. Generated
  `HTTPRequest` values now carry `redirect` ("follow" or "manual") and, for a
  streamed raw body, `duplex: "half"`; custom transports must forward both to
  `fetch`.
- Typed endpoints now authorize before they validate, in Laravel FormRequest
  order: decode, prohibited-input check, preparation, request authorization,
  binding (model binding and its resource policy), validation, then the handler. A denied caller no longer
  reaches database-backed rules such as `Unique` or `Exists`. Actor authorization
  from `RequireAuthentication(...).WithAuthorization` and
  `OptionalAuthentication(...).WithAuthorization` runs at the same stage, after
  any endpoint-level authorization, for new and idempotent requests alike.
  Authorization receives prepared, structurally decoded input that validation
  has not yet checked.
- A typed handler's outcome is authoritative. A successful result is prepared and
  written even if the request deadline expired after the handler returned, and a
  returned error (including a declared application error) is published as
  returned instead of being replaced by a timeout. Credential-bearing responses
  (browser-session cookies, token and MFA responses) remain the exception: they
  are never published after the request context ended.
- Timeouts are phase-aware. A deadline while the request body, query or
  multipart form is still being read or decoded returns 408 `request_timeout`;
  a deadline after decoding (preparation, authorization, validation, an
  unclassified handler error, opening a file source or an asset lookup) is the
  server's budget and returns 503 `unavailable`.
- `MaxConcurrentRequests` exhaustion no longer fails a burst immediately: the
  kernel queues the request in arrival order for at most
  min(`RequestTimeout`, 5s) and then returns 503 with `Retry-After: 1`. Shutdown
  ends waits at once.
- Default typed JSON response limits are now 32 MiB and 1,048,576 wire nodes
  (`DefaultResponseBytes`, `DefaultResponseNodes`), so ordinary large list pages
  no longer fail with 500. Request-body defaults are unchanged. An oversized
  response is still a 500, and its redacted diagnostic now names
  `EndpointLimits.Response` and the exceeded byte, depth or node bound.
- Optional and repeated multipart file fields treat a browser's blank file input
  (a part with `filename=""` and no bytes) as absent. Required fields still
  receive it as an empty upload; named zero-byte files remain uploads.
- Path and query codecs, URL generation, request-body reads, direct
  `contract.JSON.Encode`/`value.EncodeJSON` calls and error classification now run
  on the calling goroutine through `callback.Invoke` instead of a goroutine per
  call. Panics remain contained as internal faults with frames; `runtime.Goexit`
  in such a callback now ends the calling goroutine like any Go call (the kernel
  still releases the request). Application handlers, response preparation,
  preparation/authorization hooks, file sources and asset lookups keep the
  isolated boundary that also converts Goexit.
- Model binding resolves the bound model at the endpoint's new binding stage,
  after request authorization and before validation (Laravel route-binding
  order): a missing model is 404 and a `WithAuthorization` resource denial 403
  before body validation reports 422, and the handler receives the model from the
  single lookup. Custom transports without `HandleBound` keep resolving after
  validation; idempotent model-bound endpoints still resolve in their preparation.

#### Added

- `Endpoint.HandleBound(Binding)` (and `HandleBound(AuthenticatedBinding)` on
  authenticated, optional and signed endpoints) exposes a typed binding stage
  between request authorization and validation. The binding resolves request
  state and returns the handler that completes the request, so state such as a
  route model is resolved once.
- `Route.WithTimeout`/`Endpoint.WithTimeout` replace the kernel `RequestTimeout`
  for one route, and `WithBodyLimit` replaces `MaxBodyBytes` for one route, for
  example a large upload, without raising the server-wide default. Both apply
  after route matching, are bounded by `MaxRouteTimeout` (24 hours) and
  `MaxRouteBodyBytes`, and are inspectable through `RouteInfo.Timeout` and
  `RouteInfo.MaxBodyBytes` (server policy, not exported to client manifests). A
  longer route timeout keeps request values, client-disconnect and
  forced-shutdown cancellation, and extends the native connection read/write
  deadlines for that request.
- Within a scope, an empty route pattern (`StaticPath("")`) now names the scope
  root itself (`/api`) instead of forcing a trailing slash.
- Files that report no modification time, such as `embed.FS` assets, receive a
  strong SHA-256 content `ETag` computed once per file, so an embedded SPA shell
  can answer `If-None-Match` with 304.
- `Endpoint.WithHeaders` derives response metadata from a handler's successful
  result: `Location`, `Content-Location`, `Content-Language`, `Cache-Control`,
  `Expires`, `Last-Modified`, `Link`, `Vary` (added to framework values) and
  application `X-*` headers other than proxy, framework and security headers.
  `RouteLocation` and `EndpointLocation` build a `Location` from typed URL
  generation. Invalid or disallowed headers are internal failures; idempotent
  endpoints keep using `IdempotentEndpoint.WithHeaders`.
- `Router.WithFallback(id, handler)` runs a handler, such as a custom HTML 404
  page, for GET and HEAD requests that match no route and no SPA fallback, like
  Laravel's `Route::fallback`. Declared routes, 405 responses and other methods
  are unchanged; route inspection lists it with `RouteInfo.Fallback`.
- Declared application errors can be localized: `ErrorDeclaration.MessageDefinition()`
  returns the parameter-free `http.error.<code>` catalog signature, and
  locale-enabled applications render its translation, falling back to the
  declared message. Codes, statuses and exported metadata are unchanged.
- `ServeEvents` publishes typed server-sent events (`text/event-stream`) from a
  raw route: each event's data is checked by its `contract.JSON[T]`, events are
  flushed individually, idle streams send heartbeat comments, `EventSink.Send`
  waits on a bounded queue, and the producer is cancellation-aware and owned
  until it returns (panics and Goexit contained).
- `Stream.Progressive()` flushes each accepted chunk of a typed stream response
  to the client for incremental output, keeping its length and byte checks.

- Typed responses can declare several success statuses:
  `JSONResponses(descriptor, 200, 201)` returns a `Response[Statused[R]]`, and
  the handler selects one through `Statused[R]{Status, Value}` (zero selects the
  primary). An undeclared status is an internal failure. `EndpointInfo.Statuses`,
  the manifest, OpenAPI and the TypeScript client expose every status; the
  client returns a typed `StatusResult<200 | 201, T>` with `status` and `body`.
- `RedirectResponse(status)` declares a bodiless 301/302/303/307/308 response.
  Handlers return a `Redirect` built by `RedirectToRoute`, `RedirectToEndpoint`
  or `RedirectTo`, which accept only relative URLs on the same origin, so request
  input cannot create an open redirect. The manifest and OpenAPI describe the
  `Location` header; the TypeScript client sends `redirect: "manual"` and returns
  `RedirectResult` (`status`, `location`) instead of following it.
- `RawRequestBody(media...)` declares a streaming request body of declared media
  types. Handlers read a `RawBody` (`io.Reader` with `MediaType` and `Length`),
  bounded by the new `EndpointLimits.Raw.Bytes` (default 2 MiB) and the route
  body limit; undeclared media is 415 and read errors map to 413/408/400.
  OpenAPI publishes binary content, and the TypeScript client sends a `Blob`,
  `ArrayBuffer`, typed array or `ReadableStream`, bounded while it streams.

- `EventStreamResponse(descriptor)` makes server-sent events a typed endpoint
  response: handlers return `EventsFrom(producer)`, each event's data is bounded
  by `EndpointLimits.Response`, and `LastEventID(ctx)` supports resuming. The
  manifest and OpenAPI describe `text/event-stream` with the data type; the
  TypeScript client returns an `EventStreamResult<T>` async iterable of decoded
  `{ id?, name, data, retry? }` events.

#### Performance

- Typed JSON endpoints (100-item list response and a JSON body request,
  `BenchmarkTypedJSONEndpoint`, Apple M4 Max, `-benchtime=2000x -count=3`):
  router ~314 µs, 211 KB, 5,826 allocs per request before; ~207 µs, 114 KB,
  2,733 allocs after. Through the kernel: ~322 µs, 215 KB, 5,854 allocs before;
  ~189 µs, 116 KB, 2,752 allocs after.
- `BenchmarkRequestLifecycle`: 6.5 µs, 10.6 KB, 77 allocs before; 4.6 µs, 8.5 KB,
  45 allocs after (with preparation and authorization: 7.6 µs/95 allocs before;
  5.5 µs/63 allocs after).
- The JSON wire parser uses `encoding/json/jsontext`: decoding a 100-item document
  fell from 108 µs and 1,936 allocs to 34 µs and 1,222 allocs. Encoded responses
  are bounded by an allocation-free `jsonwire.Measure` scan (8.8 µs, 0 allocs)
  instead of a second full parse, so a response is parsed once for its schema
  check. Request bodies are read into a buffer pre-sized from `Content-Length`
  (bounded to 32 KiB before bytes arrive).
- Kernel admission and drain tracking use atomic counters; the logger,
  observation and deadline budget share one request context value; the matched
  route's metadata is built once at registration. URL generation reuses the path
  grammar parsed once by `DefinePath`.
- `LocalDownload` files and directory assets reach the native writer's
  `io.ReaderFrom` (`sendfile` on plain TCP) through the kernel's observed writer,
  with the transferred length still verified. Successful asset lookups are cached
  for one second (at most 4,096 entries; misses are never cached).

### HTTP edge, middleware and web security

#### Fixed

- Request headers that belong to the browser or the network no longer reject a
  whole request with 400:
  - Cookie reads scan the `Cookie` header pair by pair. Cookies owned by other
    scripts or applications (JSON or quoted values, non-ASCII bytes, nameless
    pairs, trailing separators) are ignored; only the requested cookie's own
    value must be valid. Repeated occurrences of the requested name read as
    absent instead of 400, because the server cannot tell which path or domain
    set each copy; prefer `__Host-` names for credentials. Input bounds are now
    64 KiB, 256 fields and 2,048 pairs.
  - `TrustedProxy` validates `X-Forwarded-For`/`Forwarded` entries only as its
    right-to-left trust walk consumes them. `ip:port`, bracketed IPv6 with port
    and bare IPv6 spellings are accepted; an `unknown`, obfuscated or malformed
    hop, or an empty field, stops the walk at the last trusted hop. Entries
    beyond the trust boundary are never inspected, so spoof resistance is kept.
  - CORS treats a malformed, repeated, opaque (`null`) or non-HTTP(S) `Origin`
    as not allowed: ordinary requests reach the handler without sharing headers
    and preflights from such origins are 403.
  - A User-Agent with control characters, invalid UTF-8 or more than 4,096
    bytes is sanitized by the new `attribution.SanitizeUserAgent` (U+FFFD
    replacement, controls removed, truncation on a rune boundary) instead of
    failing every route, including health checks.
  - A malformed, duplicated or oversized `Accept-Encoding` field is ignored and
    the response is sent uncompressed.
- Automatic ETags no longer silently disappear beyond four concurrent GETs.
  Bodies written once with their declared length (typed JSON) are hashed in
  place without a capture copy or shared slot; captures up to 32 KiB need no
  slot either. `DefaultETagConfig` allows 64 concurrent large captures, and
  another waits up to the new `ETagConfig.AdmissionWait` (default 100 ms, at
  most 5 s) before passing through without a validator.
- CSRF adds `Vary: Sec-Fetch-Site, Origin` only to unsafe requests it actually
  checked, so cacheable GET responses no longer vary on them, and it no longer
  clones every request header per unsafe request.
- Pagination endpoints with `Config.Links=PublicLinks` now fail `ApplyMiddleware`
  when no `PublicURLs` covers them, instead of failing each request with a 500.
- Model-binding resolvers validate their declaration once at registration;
  request lookups and nested `Then` parents no longer revalidate every ancestor
  per request.

#### Changed

- `RateLimitByIP` keys IPv6 clients by /64 network (IPv4 stays per address), so
  one subscriber cannot bypass a limit by rotating addresses inside its prefix.
  Every 429 from `RateLimit`/`RateLimitByIP` carries `Retry-After` of at least
  one second alongside the `X-RateLimit-*` headers.
- `ApplyMiddleware` rejects actionable ordering hazards at assembly: rate
  limits, CSRF, `PublicURLs`, browser sessions and credential-request checks
  declared before `TrustedProxy` (or a route that installs `TrustedProxy` under
  such a global policy), and routers whose signed routes, `PublicLinks` pages or
  `RequirePublicURLs()` routes are not covered by `PublicURLs`.
- Numbered pagination bounds offset depth: `pagination.Config.MaximumPage`
  (default `DefaultMaximumPage`, 10,000) rejects deeper pages with 422 at
  `/query/page`, and navigation never links beyond it.
- Compression now waits briefly for an encoder slot when a client refuses
  identity, then returns 503 with `Retry-After`, instead of failing immediately.
- Built-in cookie codecs, the system signing clock and framework cookie
  encoding run on the calling goroutine (`callback.Invoke`); custom, text and
  enum codecs and application lookups keep the isolated boundary.

#### Added

- `Cookie.Encrypted(NewCookieEncrypter(keyring, clock))` returns an
  `EncryptedCookie[V]` sealed with the application's encryption keyring
  (AES-256-GCM, `foundry.cookie.v1` purpose), bound to the cookie's name and
  scope, with its expiry inside the ciphertext and key rotation through retained
  keys. Invalid values share `ErrInvalidEncryptedCookie` and read as BadRequest.
- `CORSConfig.OriginPatterns` allowlists exact custom-scheme origins
  (`capacitor://localhost`, `tauri://localhost`, `chrome-extension://<id>`) and
  validated subdomain wildcards (`https://*.example.com`; no bare `*`), and
  `CORSConfig.Paths` selects a complete policy per path prefix.
- `PublicURLConfig.ExemptPaths` serves exact probe paths (for example
  Kubernetes probes with `Host: <pod-ip>`) without host admission, and
  `PublicURLConfig.AllowedPatterns` admits tenant subdomains such as
  `https://*.example.com`; signed URLs verify on the admitted tenant origin.
  `RequirePublicURLs()` marks custom routes that generate public URLs.
- `RateLimitByIPWith(limiter, IPRateLimitOptions{...})` configures the IPv4 and
  IPv6 key prefixes, and `RateLimitExceeded(decision)` lets a handler that calls
  a limiter itself return a 429 with the same retry and quota headers.
- Signed routes and endpoints support `WithPermanentLinks()` with
  `PermanentURL` (explicit opt-in, no expiry), origin-relative `RelativeURL`
  links, and `WithIgnoredParameters(...)` for unauthenticated parameters such as
  mail-client analytics, which are removed before verification. Existing
  absolute expiring links remain valid.
- `LocaleWith(catalog, LocaleNegotiation{QueryParameter, Cookie, Preferred})`
  resolves the request locale from an explicit query parameter, a cookie and an
  application preference (such as an authenticated user's stored locale) before
  the context locale and `Accept-Language`, each validated against the catalog.
- `CacheControl(CachePolicy{...})` declares typed `Cache-Control` (private or
  public, `max-age`, `s-maxage`, `stale-while-revalidate`, `stale-if-error`,
  `no-cache`, `must-revalidate`, `immutable`, `no-store`) and optional
  `Expires` for GET/HEAD, composes with ETags, never overrides an outer
  `no-store` and downgrades public policies for requests with `Authorization`.
- `SecurityHeadersConfig` gains typed `CrossOriginOpener`, `CrossOriginEmbedder`,
  `CrossOriginResource` and `Permissions` (a structured `Permissions-Policy`).
- Model-binding `Resolver.WithMissing` customizes the error for an absent model.
- Pagination `Config.EdgeLinks` adds `first` and (numbered) `last` links and
  `Config.PageWindow` adds a bounded `window` of numbered page links; both are
  opt-in and absent from the wire when disabled.

#### Performance

- Compression reuses gzip and Brotli encoder state through per-middleware pools
  (`BenchmarkCompressionEncoderReuse`, 16 KiB JSON-like body, Apple M4 Max,
  `-count=3`): gzip fastest ~60 µs, 814 KB, 14 allocs per response before;
  ~6.7 µs, 36 B, 1 alloc after. Brotli level 4 ~60 µs, 793 KB, 12 allocs before;
  ~9.8 µs, 38 B, 1 alloc after.

### Authentication, authorization and encryption

#### Fixed

- Configured browser and token guards now share one application-owned
  authorization registry (`application.AuthorizationKey`). Previously each guard
  built a private registry containing only itself, so `WithPermissions` failed at
  router assembly, `Policy.Authorize` returned `Missing` (HTTP 500), and
  `auth.RegisterAuthorization` contributions were ignored. Contribute policies,
  permissions and hooks with `application.Authorization(id,
  application.Authorize(declaration)...)`; `Registry.With` derives the guard's
  registry without modifying the shared one and shares its callback capacity.
- Password lockout can no longer be used to lock a victim out indefinitely.
  `PasswordLogin.WithLockout` requires a client-aware `lockout.DefineLogin`
  declaration (single-key `lockout.Define` throttles are rejected with
  `fault.Invalid`). `lockout.DefaultLimits()` counts failures per account and
  trusted client IP (5 / 15 min) with an account ceiling (50 / 15 min); IPv6
  clients group per /64. An optional per-address ceiling
  (`Limits.Address`, for example `value.Set(lockout.DefaultAddressPolicy())`) is
  off by default because it locks every user behind a shared address. Success
  never clears the address ceiling. Redis and memory backends keep their per-key
  semantics.
- Pending-MFA sessions and challenge tokens no longer consume a subject's
  active-credential cap; they have their own `MaxPendingPerSubject` (default 8)
  and replace the oldest pending credential. Reaching the cap under the new
  `auth.RejectNew` policy returns the typed `auth.CredentialLimit` (HTTP 409
  `conflict`) instead of `fault.Conflict` (HTTP 500). Expired credentials never count.
- Losing the rehash compare-and-swap after a correct password (for example to a
  parallel login) no longer rejects the login: the model is reloaded once and the
  password verified against the winning hash. A password changed mid-attempt is
  rejected without counting as a lockout failure.
- Removing a scope from a token binding's ceiling no longer breaks `List` or makes
  `Refresh` commit and then return `Unauthenticated`. The current ceiling is
  intersected with stored grants (`AccessScopes.Intersect`); stored grants are
  never widened.
- A refreshed token family keeps the previous access token valid until its own
  expiry or `token.Config.AccessGrace` (default 30 s, zero disables) after the
  refresh, so parallel in-flight requests no longer fail with 401. A refresh whose
  commit completed is returned even when the deadline passes right afterwards.
- `http.NewAuthentication` rejects cookie credential sources without origin/CSRF
  protection. Use `http.NewCookieAuthentication` (or the browser-session adapter),
  or opt out explicitly with `CookieCredential(...).WithoutOriginProtection()`.
- An account with an enrolled second factor can no longer bypass MFA when
  `PasswordModel.RequiresMFA` forgets the factor state. Link the factors with
  `login.WithSecondFactor(factors)`; `mfa.Factors` implements `auth.EnrolledFactors`.
- A committed password reset can clear the account's login lockout with
  `passwordreset.WithLockout(reset, throttle, loginKey)` (best effort; it never
  undoes the reset).
- Email verification works when the login provider only admits verified accounts:
  `emailverification.Model`, `passwordreset.Model` and `challenge.Model` accept an
  optional `Eligible` callback that replaces provider eligibility for that flow.
- Recovery link requests no longer reveal account existence through latency.
  After the synchronous recipient-quota admission, lookup, issuance and delivery
  run in a bounded dispatch owned by `challenge.Requests`; the request returns
  immediately. Register `requests.Close(ctx)` for shutdown; `Wait(ctx)` observes
  completion. Dispatch capacity exhaustion reports `fault.Overloaded` before lookup.
- Auth registry, credential store, lockout and hashing capacity queue for a bounded
  wait and then report `fault.Overloaded` (HTTP 503) instead of failing immediately
  with `fault.Conflict` (HTTP 500).
- Token pruning removed only 16 families per call with per-family lookups.
  `token.MaxPruneFamilies` is now 1024 and session/token pruning run as one
  index-backed, set-based statement that skips rows locked by concurrent work and
  rechecks expiry on each deleted row.

- An impersonation session can no longer mint credentials for its subject:
  `Sessions.CurrentProof` and `auth.CurrentProof` refuse it (new
  `auth.AttachImpersonatedCredential` marks such credentials), and
  `Sessions.RevokeOthers` refuses it, with `auth.ImpersonationForbidden`.
- `Impersonation.Resume` rotates the actor's original session in place instead
  of issuing a new one, so its creation time, absolute expiry and remember policy
  are kept and repeated impersonation cannot extend it
  (`session.ResumptionBackend.ResumeActor` replaces `ConsumeActorIn`).
- Revoking or expiring the actor's session now ends the impersonations it
  started: session lookup and listing require the recorded actor session to still
  exist and be live, so logout, `RevokeAll` and password resets (`RevokeAllIn`)
  end them. Actors and targets must share one session store.
- `After` authorization hooks now also run after a `Before` hook allowed, so a
  global veto (read-only mode, suspension) applies to super-administrators.
- Session capacity validation accounts for three classes (full, pending MFA,
  impersonation): `MaxPerSubject + 2*MaxPendingPerSubject <= MaxSessions`. A
  committed or joined revocation no longer fails because of its count, which
  could roll back a password reset (`RevokeAllIn`) or report an error after a
  standalone `RevokeAll`/`RevokeOthers` committed.
- `Lifetime.TouchInterval` is capped at half the idle lifetime, so sessions with
  an idle window of one minute or less no longer log out active users.
- `Tokens.Revoke`/`Logout` with the previous generation's access token (still
  valid during `AccessGrace`) now revokes the family instead of doing nothing.
- Events emitted from an impersonation session (such as `EventLogout`) now carry
  `Event.Impersonator`.
- Password confirmation can be throttled per subject with
  `PasswordLogin.WithConfirmationLockout` (`auth.ConfirmationKeys()`), so a stolen
  session cannot brute-force the password through `Confirm`.
- Session and token column checks added in this release are created `NOT VALID`
  and validated by separate migrations (`000004_validate_session_state`,
  `000003_validate_token_state`), so no table scan runs under an exclusive lock.
- Nested policy or guard evaluation inside another callback of the same auth
  scope reuses the parent's callback slot instead of queueing behind it (a stall
  of up to five seconds on a saturated registry).
- The password-hashing guide documents that imported bcrypt hashes make login
  timing distinguishable until they are rehashed.

#### Changed

- `password.DefaultConfig().MaxConcurrent` is 4 (was 2). Each Argon2 check
  allocates its hash's memory cost: about 256 MiB with the defaults, 512 MiB worst
  case with legacy hashes at the verification ceiling.
- Session issuance defaults to `auth.EvictOldest`: at the cap, a new login revokes
  the subject's oldest session. Tokens default to `auth.RejectNew`. Set
  `session.Config.Limit` / `token.Config.Limit` to choose.
- Token refresh keeps only the current and previous generation rows; older refresh
  digests move to a compact consumed set that still detects reuse and revokes the
  family. Migration `000002_token_device_refresh_history` adds it with nullable
  device columns; existing rows stay valid.
- Removing a scope from a token binding's ceiling withdraws it from existing tokens
  instead of making them fail verification.

#### Added

- `sessions`/`tokens` `.Current(ctx)`, `.RevokeCurrent(ctx)`, `.RevokeOthers(ctx)`
  ("log out other devices") and `.CurrentProof(ctx)`, which keeps the request's
  scope grants when minting a token. Adapters use `auth.CredentialSlot`,
  `auth.AttachCredential`, `auth.CurrentCredential` and `auth.CurrentProof`.
- Authorization hooks: `auth.DefineBefore` (first `Allow`/`Deny` decides, for
  example a super-administrator) and `auth.DefineAfter` (veto only). Policies can
  return `auth.NewDenial(code, message)`; `Inspect` returns an `auth.Decision`, and
  `Authorize` returns the `*auth.Denial`, which remains `auth.Forbidden`.
  `auth.DefineGuestPolicy` evaluates anonymous requests with an omitted model.
- Authentication lifecycle events through `auth.Observer`: `EventLogin`,
  `EventLogout`, `EventOtherDevicesLoggedOut`, `EventFailed`, `EventLockout`,
  `EventPasswordReset` and `EventVerified`, via `WithObserver` on sessions, tokens,
  `PasswordLogin`, `passwordreset.Reset` and `emailverification.Verification`.
- Password confirmation: `PasswordLogin.Confirm(ctx, model, password)`,
  `sessions.ConfirmCurrent(ctx)`, `sessions.RequireConfirmed(ctx, within)`
  (returns `auth.ConfirmationRequired`, HTTP 403) and the route middleware
  `browser.RequirePasswordConfirmation(within)`.
- A post-authentication middleware stage: `WithActorMiddleware` on
  `RequireAuthentication`, `OptionalAuthentication` and their native route forms
  runs after the guard, scopes and permissions and reads the concrete actor from
  the scope cache. `http.RateLimitByActor(limiter, guard)` keys quotas by the
  authenticated model (`http.ActorKeys()`), falling back to the client IP for
  anonymous requests and failing when installed before authentication.
- Imported bcrypt (`$2a$`/`$2b$`/`$2y$`, bounded by `password.Config.MaxBcryptCost`)
  and Argon2i hashes verify and are rehashed to Argon2id on the next login.
- Device metadata (trusted client IP and user agent, 512 bytes) on sessions and
  token families: `Info().Device()`. Session migration
  `000002_session_device_confirmation` adds nullable device and confirmation columns.
- `token.Config.Prefix` prepends a scanner-friendly prefix (for example `acme_pat_`)
  to new token secrets; only the random part is hashed, so existing tokens keep working.
- `sessions.PruneExpired(ctx, batch, maxBatches)` and `tokens.PruneExpired(...)`
  prune in bounded batches until a short batch and report the removed count, for
  use from a scheduled handler or the configured housekeeping schedule. The auth
  packages do not depend on the scheduler.
- `sessions.Logout`/`tokens.Logout` revoke a presented credential and report
  `EventLogout`.

- Application encryption keys: `Encryption.KeyID`, `Encryption.Key` and
  `Encryption.Previous` (retired keys by ID) are generated sensitive settings that
  load from secret files (`NAME_FILE`) or environment. `application.New` parses
  them before acquiring resources and provides one `encryption.Keyring` through
  `Services.Encryption()`; `Services.CookieEncrypter()` uses it for encrypted
  cookies.
- `Features.Auth.MFA` constructs the MFA store from the configured database and
  the application key ring (`Services.MFA()`); it requires encryption keys.
- Password-free MFA key rotation: `(*mfa.Store).ReencryptStale(ctx, cursor, limit)`
  re-encrypts stored factors under the active key in bounded batches through the
  new `mfa.RotationBackend` contract (implemented by the PostgreSQL adapter),
  with conditional replacement so it is safe alongside logins.
  `application.MFACommand()` provides `mfa reencrypt`. `encryption.EnvelopePrefix`
  identifies envelopes of a key.

- Session impersonation: `session.NewImpersonation(actors, targets, maximum)`
  with `Start` (bounded, never remembered, marked with the actor's identity, guard
  and session; counted apart from the target's own sessions; ended with the
  actor's own session), `Actor`, `Resume` (returns to the actor's session under a
  fresh secret, keeping its lifetime) and `Stop`. Impersonated sessions are refused by
  `Sessions.RequireNotImpersonating` and `ConfirmCurrent`
  (`auth.ImpersonationForbidden`, HTTP 403). `EventImpersonationStarted` and
  `EventImpersonationStopped` carry `Event.Impersonator`. Browser guards add
  `Impersonate`, `StopImpersonating` and the `RefuseImpersonation()` route
  middleware. Migration `000003_session_impersonation` adds nullable columns.

- Social login: `auth/oauth` implements the OAuth 2.0 authorization-code flow
  with PKCE (S256), state and nonce, exact redirect URIs and token exchange
  through a destination-restricted `httpclient.Client`. OpenID Connect ID tokens
  are verified with the standard library (RS256/ES256 against a cached JWKS that
  follows key rotation; iss, aud/azp, exp, iat, nonce and sub checks). `Google`
  (OpenID Connect) and `GitHub` (user API, verified primary email) presets return
  a typed `Profile`. `oauth.Flow` keeps the pending request in an encrypted,
  short-lived cookie; `Client.Begin`/`Complete` support server-side storage.

#### Performance

- Cookie-session request authentication is one schema-qualified joined read with
  no transaction, row lock or `SET LOCAL search_path` (previously about 7-8 round
  trips and a `FOR UPDATE` on the per-user subject row that serialized parallel
  requests). Sliding activity is written at most once per max(1 min, idle/20) with
  a conditional update. Session listing no longer locks the subject.
- Bearer-token verification is one joined read without a transaction; listing,
  revoke-all, capacity enforcement and pruning are set-based instead of per-family
  queries.

### Database runtime and typed queries

#### Fixed

- A `Rows` stream the caller forgets to close no longer keeps its pool
  connection, transaction or session scope until shutdown: the end of the query's
  context now closes it, returning the connection and releasing the resource owner.
- A client disconnect while a transaction commits no longer turns the commit into
  `CommitUnknown`. After the callback succeeds, `COMMIT` (and rollback) run
  detached from caller cancellation and bounded by the new
  `PoolConfig.CommitTimeout` (default 30s).
- Transaction, savepoint and session callbacks that fail with an application error
  now return that error unchanged after a confirmed rollback, instead of a
  `database transaction: query_failed` wrapper. Failures containing a database
  error are still `*database.Error` values with the outcome; an unconfirmed
  rollback joins the application error with a `NoCommit` database error.
- `migrate up` failures now name the migration and the next safe action (not
  committed and retryable, lock wait exceeded, unknown outcome requiring
  `migrate status`), and checksum drift and reconciliation messages keep their own
  text instead of `database session: query_failed`.
- `RequireFirst`, locked-query `RequireFirst`, model-write `RETURNING` cardinality,
  relation cardinality and per-model identity failures now return
  `*database.Error` values (`NotFound`, `TooManyRows`) through `database.NewError`,
  so `errors.As` works like it does for `ScanOne`.
- Readiness probes no longer queue behind application work: each endpoint is
  pinged through one dedicated probe connection outside `MaxOpen`, so a saturated
  pool cannot make every replica unready at once.
- Typed queries now prove observer absence through a module-sealed owner
  capability implemented by `DB`, `PrimaryExecutor`, `Session` and `Tx`, so a
  pinned `db.Primary()` executor takes the same write and read paths as its pool
  instead of the slower wrapper fallback, without taking the pool lock per call.
- `Each`, `Chunk`, `EachChunked` and the batches `Each` uses for eager loading,
  retrieval hooks/observers and unknown executors no longer advance by
  `LIMIT`/`OFFSET`: after the first batch they continue after the last delivered
  row's ordered values and primary key, so rows deleted or inserted behind the
  traversal cannot shift later batches. Only unkeyable orders (computed
  expressions, explicit NULL placement, fields without generated getters) and
  complete-record results with joins, grouping or `DISTINCT` still use offsets.
- Keyset and cursor predicates no longer add `OR col IS NULL` alternatives for
  columns declared NOT NULL, and use a single row comparison
  `(a, id) > ($1, $2)` when every key is NOT NULL and shares one direction.
- Counts of paginated projections, sets and value queries omit the outer
  `ORDER BY` unless `DISTINCT ON` or a `Limit`/`Offset` window depends on it.
- PostgreSQL errors now classify `57014` as `QueryCanceled` (previously
  `Canceled`, which remains caller-context cancellation), `55P03` as
  `LockNotAvailable`, `23P01` `ExclusionViolation`, `23001` `RestrictViolation`,
  `25006` `ReadOnlyTransaction`, `25P03` `IdleInTransactionTimeout`, `55006`
  `ObjectInUse`, class 22 data errors (`StringDataRightTruncation`,
  `InvalidTextRepresentation`, `NumericValueOutOfRange`, `InvalidDatetimeFormat`,
  `DatetimeFieldOverflow`, `DivisionByZero`, otherwise `DataException`) and class
  53 as `InsufficientResources`.
- Per-model batch writes (`CreateEach`, `UpdateEach`, `DeleteEach`,
  `RestoreEach`, `ForceDeleteEach`, relation detachment), lookup writes, pivot
  attachment and factory `CreateMany` no longer open a savepoint per row. Rows run
  directly in the operation's one transaction (or the caller's single savepoint),
  avoiding PostgreSQL subtransaction-cache overflow beyond 64 rows.
- A PostgreSQL schema-scoped connection now lists `pg_temp` explicitly after the
  application schema, so session temporary tables cannot shadow application tables.
- A pool acquisition that times out while the caller's context is still live now
  also matches `fault.Overloaded` (keeping code `DeadlineExceeded`), so HTTP
  answers a saturated pool with a retryable 503 and `Retry-After: 1`.

- An invalid polymorphic `MorphName` (for example `App\Models\Post` or
  `blog-post`) no longer validates once a generated relation set binds the
  relationship: `MorphMany`, `MorphOne`, `MorphTo`, `MorphToMany` and
  `MorphedByMany` keep the declaration error through `Bind`, so every query, load
  and pivot write through them fails with `fault.Invalid`.
- `HasOneThrough(...).OfMany(...)` (and `LatestOfMany`/`OldestOfMany`) chooses one
  target per parent across all its intermediate rows instead of one per
  intermediate row, which failed with `too_many_rows` for a parent with several
  intermediates.
- `Sync`, `SyncWithoutDetaching`, `Toggle`, `DetachMany`, `DetachAll` and
  `UpdateExistingPivot` lock the source row `FOR NO KEY UPDATE`, so concurrent
  calls for one source serialize: two concurrent `Sync`s no longer leave the union
  of both sets and two `Toggle`s no longer create duplicate links. `AttachMany`
  keeps its shared lock.
- Sticky reads measure the window from the write's completion: `Exec`, primary
  `Query` streams (on close), `Session.Exec`/`Session.Query` and committed (or
  commit-unknown) transactions mark the request scope when they finish as well as
  when they start. `DB.Query` and `Session` statements previously did not mark at all.
- An upsert's `DO UPDATE` now honors the destination model's global scopes: a
  conflicting row outside them is not changed, and an unconditional update that
  skipped such a row fails with `fault.Conflict` instead of returning an omitted
  result.
- Generated queries register `DefineGlobalScopes` as a lazy scope source
  (`query.Definition.WithGlobalScopeSource`) evaluated on first compilation, so a
  scope may use the model's own relations (for example
  `DeskRelations().Office.Exists()`); calling them from `DefineGlobalScopes`
  previously deadlocked the generated query declaration. Regenerate models that
  declare global scopes.
- `CreateOrFirst`/`InsertOrFirst` resolve only a unique violation of the model's
  own `INSERT` on a unique index of the model's table, classified with bounded,
  isolated error inspection. Unique violations from hooks, observers and their
  writes, errors that merely claim to be one, and trigger violations on other
  tables are returned instead of being answered with an unrelated existing row.
- A schema-scoped PostgreSQL pool again discards, on its next checkout, a
  connection whose session created temporary objects, so session tables never
  reach another borrower. When the scope is unchanged this costs one
  simple-protocol catalog call per checkout; `set_config`/`pg_namespace` still
  run only when the scope changed.
- `foundry generate` validates a model's `DefineGlobalScopes` during discovery: a
  pointer receiver or any signature other than
  `func (M) DefineGlobalScopes() []query.GlobalScope[M]` is reported against the
  method instead of producing uncompilable generated code.

#### Changed

- `DefaultPoolConfig` now retains 16 idle connections (equal to `MaxOpen`, was 2)
  to avoid connection churn under steady load; `MaxIdleTime` still closes idle
  connections after a burst. The PostgreSQL adapter recycles each connection at a
  random point in the last fifth of `MaxLifetime`.
- `Start` retries transient connection failures with jittered backoff within the
  new `PoolConfig.StartupTimeout` (default 15s; zero makes one attempt).
  Authentication failures still fail at once.
- A hook-free `Create` on a pool (`*database.DB` or `db.Primary()`) is one
  autocommit `INSERT … RETURNING` statement instead of `BEGIN`, statement and
  `COMMIT`. A failure after the statement was sent — including an undecodable
  returned row — is now reported as `CommitUnknown`/`Unknown` with the hydrated
  candidate in `query.WriteError`, because the row may have been committed.
  Updates, deletes and application wrappers keep their transaction.
- `In`/`NotIn` on columns with integer, text, boolean, UUID, numeric, date or
  timestamptz codecs bind one array parameter (`= ANY($1)` / `<> ALL($1)`)
  instead of one parameter per value; lists are bounded by
  `query.MaxMembershipValues` (1,048,576) instead of the 65,535-parameter limit.
- Declared JSON property names are compiled as escaped SQL literals and a scalar
  directly below a property uses `->>`, so `(col ->> 'key')` expression indexes
  match typed JSON predicates under generic plans. Array indices, map keys and
  compared values remain bind parameters.
- An empty `And()`/`HavingAnd()` is now TRUE and an empty `Or()`/`HavingOr()`
  FALSE instead of an invalid-query error, for dynamically built filters.
- `migrate.PostgresConfig` gains `StatementLockTimeout` (default 10s, set as
  `lock_timeout` on the migration session) and `StatementTimeout` (default off).
- Generated `QueryX()`, `XFields()`, `XRelations()` and `XAggregates()` build
  their immutable metadata and hydration codecs once per process through
  `query.Memo`; `DefineRelations`/`DefineAggregates` therefore run once.
  Regenerate models.

- `FirstOrCreate`, `UpdateOrCreate` and `CreateOrFirst` now default omitted
  draft fields from the lookup's top-level equality filters and active global
  scopes (explicit draft inputs still win; mutator fields are skipped).
- `query.RelatedValue(relation, aggregate)` computes a typed relation aggregate
  per source row as a correlated subquery, for ordering (`Desc()`), filtering
  (`query.OrderRow(...).Gte(n)`) and projections without a model slot;
  `query.WithValue(q, value).All` returns `[]query.Annotated[M, V]` model/value
  pairs in one statement.
- `HasOne(...).LatestOfMany()`, `OldestOfMany()` and `OfMany(orders...)` load one
  chosen target per parent with `DISTINCT ON`; relationship filters take part in
  the choice.
- `query.HasManyThrough` and `query.HasOneThrough` reach targets through an
  intermediate generated model (`query.ModelQuery`), with the intermediate's soft
  deletion and scopes applied to loading, `WhereHas` and aggregates.
- Polymorphic relationships with a typed morph map: models declare
  `MorphName() query.MorphName` once; `query.MorphMany`, `MorphOne`,
  `MorphTo` (one typed slot per parent type, loaded empty for other types),
  `MorphToMany` and `MorphedByMany` read the name from the model type, and pivot
  writes fill it automatically.
- Read-your-writes routing: `database.WithStickyReads(window)` (configured
  `sticky_read_window`, `postgres.RoutingConfig.StickyReadWindow`) routes reads in
  a `database.StickyReads(ctx)` request scope to the primary for the window after
  a write; `database.StickyReadsHandler` installs a scope per HTTP request.
- Model pruning: `database/prune` declarations (`prune.Model(name, query,
  prune.Lifecycle|prune.Mass)`) remove obsolete models in bounded batches, each
  one transaction (`Query.PruneBatch`), with `prune list`/`prune run` database
  commands; soft-delete models are force-deleted within the selection's
  visibility.
- Optional `Down` SQL on migration definitions (excluded from the checksum;
  transactional migrations only) and `Postgres.Rollback`/`RollbackPlan`, exposed
  as `migrate rollback --step N`, which prints the plan unless `--confirm` is
  given and refuses irreversible, drifted or dependency-violating selections.

#### Added

- `postgres.Config` `StatementTimeout`, `LockTimeout` and
  `IdleInTransactionSessionTimeout` set the matching server limits once per
  connection; configured connections expose `primary.statement_timeout`,
  `primary.lock_timeout`, `primary.idle_in_transaction_session_timeout`, the
  `read.` equivalents, `primary.pool.commit_timeout` and
  `primary.pool.startup_timeout`.
- `database.Retry(ctx, transactor, policy, fn)` with `RetryPolicy` and
  `DefaultRetryPolicy` re-runs a whole transaction only after a confirmed rollback
  caused by a serialization failure or deadlock, with jittered backoff and bounded
  attempts; unknown outcomes are never retried.
- `database.WithQueryObserver` reports every completed statement (role,
  operation, SQL text, duration, pool wait, rows, code/SQLSTATE; never argument
  values). `WithSlowQueryLog(logger, threshold)` and
  `WithSlowQueryThreshold(threshold)` (bound to the application logger by a
  database Module; `postgres.RoutingConfig.SlowQueryThreshold`, configured
  `slow_query_threshold`) log slow statements with a fingerprint instead of SQL
  text. `PoolStats` adds `MaxIdleClosed`, `MaxIdleTimeClosed` and
  `MaxLifetimeClosed`.
- `query.True`/`query.False`, `NotIn`, `Between`, `StartsWith`, `EndsWith`,
  `IStartsWith`, `IEndsWith` and `ILike` (escaped like `Contains` except `ILike`),
  `Order.NullsFirst`/`NullsLast`, and JSON `Length()` and path `Contains()`.
- `relation.Many`/`Through` `Len()` and `All()` read loaded collections without
  copying them.
- Generated `CreateOrFirst` (`Query.InsertOrFirst`) creates through the ordinary
  lifecycle in its own savepoint and, when a unique constraint rejects the insert,
  returns the first model matching the query, resolving concurrent creation of the
  same unique key under READ COMMITTED.
- `*database.Error` contributes operation, code, SQLSTATE, constraint and outcome
  to redacted `errordiag` diagnostics.

- Model global scopes: `query.NewGlobalScope` (fixed typed predicate) and
  `query.NewContextScope` (predicate read explicitly from the executing request's
  typed context, failing closed when absent), declared by a model's
  `DefineGlobalScopes` method and attached by generation. They join the single
  effective-predicate owner with soft deletion, so reads, pagination, chunking,
  counts, relations, `WhereHas`, aggregates, joins, subqueries and all updates
  and deletes apply them. `WithoutGlobalScope(scope)`/`WithoutGlobalScopes()`
  opt out per query or relation; execution rebinds context scopes to each call's
  context, and `WithScopeContext(ctx)` serves `Compile`.
- Many-to-many `AttachMany`, `Sync`, `SyncWithoutDetaching`, `Toggle`,
  `DetachMany`, `DetachAll` and `UpdateExistingPivot`, returning typed
  `query.PivotChanges` (attached, detached and updated pivot models). Without
  pivot hooks they run a few set-based statements; with hooks each changed pivot
  runs its ordinary lifecycle, like `Attach`/`Detach`. Generated drafts gain
  `FoundryUpdateMutation` (`query.UpdateDraft`).
- Generated set-based `UpdateAll`, `DeleteAll` (soft-delete aware),
  `ForceDeleteAll`, `Increment` and `Decrement` (typed `Field.By(delta)`
  adjustments with an optional extra draft), returning affected counts. They run
  one statement without per-model hooks and are rejected on hooked models unless
  the query acknowledges `WithoutModelHooks()`.

#### Performance

- Pool owner acquisition/release and hot `Observers()`/`Clock()` reads are
  lock-free after `Start`; model writes and `Each` no longer call `Stats()`.
- A schema-scoped PostgreSQL connection validates its `search_path` once when it
  is established; checkouts re-establish it only when the server reports a
  change (PostgreSQL 18), instead of running `set_config`/`pg_namespace` on every
  checkout. An unchanged checkout still makes one simple-protocol
  `pg_my_temp_schema()` call to discard sessions holding temporary objects.
- Model declarations are validated once per generated query value instead of on
  every compiled statement; identifier checks use a byte scan instead of a
  regexp; nested relation trees are validated once per load instead of per level
  and key batch; eager loading copies the parent slice once per load and adopts
  loaded groups without re-copying them.

### Cache, Redis, leases, pub/sub, idempotency and rate limits

#### Fixed

- `Remember` no longer fails a request when publishing a successfully loaded value
  to the cache fails. The owner and its followers receive the value, the write is
  not retried, and the failure is counted (`Stats().WriteFailures`) and logged
  through `cache.WithLogger` with the cache family and a redacted diagnostic.
  Application assembly passes its configured logger automatically. A stale owner
  whose fill crossed an invalidation still returns its loaded value, but its
  publication is rejected, never stored. A coordinated fill whose lease release
  (or renewal) failed after a successful load also returns the value; the lease
  manager keeps reporting the cleanup error.
- A `Remember` owner's request cancellation no longer fails its followers. The
  loader runs in a store-owned fill under a context that keeps the caller's values
  but not its cancellation; cancelling any caller ends only that caller's wait, and
  a loader that failed only because it waited on the owner's ended request context
  makes followers elect a new owner (up to three attempts). The store-wide
  `Config.Timeout` bounds each backend step but no longer caps loaders.
- Tagged pure reads (`Get`, `Exists` and the lookup inside `Remember`) no longer
  return `fault.Conflict` while a tag or namespace invalidation runs. They re-resolve
  their snapshot up to three times and otherwise report a miss. Writes that cross an
  invalidation keep returning `Conflict` (Redis direct writes now resolve the
  snapshot inside the write, see Performance).
- A stored cache value larger than the current `MaxValueBytes` (written under an
  earlier, larger bound) or a Redis entry of the wrong type or shape is now a
  miss that a later write replaces and `Forget` removes, instead of an error that
  blocked the key. Corrupt tag metadata still fails with `fault.Invalid`.
- Cache, lease and invalidation wrappers keep the framework classification of their
  cause (`Overloaded`, `Invalid`, `Timeout`, `Closed`, `Conflict`) instead of
  re-wrapping it as `Internal`, so `errors.Is` works and HTTP maps capacity
  exhaustion to 503.
- A Redis command whose reply arrived is reported as a success even if its deadline
  expires immediately afterwards; it is no longer classified as
  `context.DeadlineExceeded`.
- Redis tag and namespace metadata now expires after 30 days without use. Every read
  or write refreshes it, a finite tagged write keeps it alive at least as long as the
  entry, and metadata written by earlier releases without a TTL receives one on first
  use. An idle expiry turns old entries into misses without resurrecting them.
- Coordinated fill followers re-read the cache at the lease manager's `PollInterval`
  (with jitter) between lease attempts, so a value published by another instance is
  returned as soon as it appears instead of failing with `DeadlineExceeded` while a
  slow loader still holds the lease.
- A lease key without expiry, previously treated as corrupt forever, can now be
  repaired with `Leases.ForceRelease`.
- Rate-limit policy changes no longer fail with `fault.Conflict`/503 until the old
  window expires. A live bucket switches to the requested policy immediately and
  keeps admitted usage, capped at the new capacity. Peeks persist nothing; a denial
  persists only a conversion whose window ends later, so tightening 10 per minute
  to 10 per hour keeps a spent key limited for the hour instead of reopening when
  the old minute ends. Memory and Redis clamp a backward clock step instead of failing or
  reopening quota, and Redis buckets written by earlier releases stay valid and are
  rewritten in the new versioned format on their next admission.
- Idempotent operations of one caller no longer serialize on a caller-wide
  advisory lock for the whole handler and commit. The retention quota is checked
  after the claim with a bounded indexed read and takes no lock; concurrently
  executing new operations of one caller can exceed `MaxRetainedPerCaller` by at
  most their own number.
- `MaxRetainedPerCaller` counts only unexpired committed outcomes; expired records
  awaiting pruning no longer consume quota.
- An idempotency lock timeout is reported as in progress (409) only while the claim
  waits for an uncommitted request with the same scoped key. The runner restores
  the caller's `lock_timeout` before the handler, and the handler's own lock
  timeout rolls back and returns retryable `Unavailable` (503).
- A changed idempotency result contract no longer makes every retained outcome
  unavailable until expiry. Replay returns the stored representation when the
  current codec accepts it; an incompatible outcome stays unavailable and is never
  re-executed early.
- File and PostgreSQL caches no longer let expired records block writes. A write
  that does not fit first reclaims up to 256 expired records and is rejected only
  when the cache is still full of live data.
- A file or PostgreSQL cache entry larger than the current value bound, or a corrupt
  file record, is now a miss that writes replace and `Forget` removes, instead of an
  error that blocked the key. Prune passes skip and count corrupt or foreign files
  instead of stopping.
- PostgreSQL cache expiry now uses the database clock, so application servers no
  longer need synchronized wall clocks. An injected `Config.Clock` remains for
  deterministic tests.
- `cache.Store` now has `Close(ctx)`/`Done()`. Remember loaders whose callers
  have gone, uncoalesced loads and Flexible background refreshes are Store-owned
  fills: `Close` rejects new operations with `fault.Closed`, cancels running fills
  and waits for them, bounded by its context. Configured applications close each
  cache store during shutdown before its backend, lease manager or database, so
  shutdown no longer closes Redis or PostgreSQL under a running loader.
- A Remember loader that reloads its own key while `MaxFills` is exhausted (an
  uncoalesced load) now fails with `fault.Cycle` instead of recursing without
  bound.
- `Topic.Supervise` no longer stops for good during a Redis outage. A connection
  that closes or reports a protocol anomaly while a subscription is being
  established is a retryable `pubsub.ErrDisconnected`; only the explicit shutdown
  sentinel `pubsub.ErrClosed` (and invalid input) ends supervision.
- Redis `Hash.GetAll` and set `Members` no longer fail on a Redis server with a
  non-C collation locale: the adapter sorts fields and members by bytes instead of
  relying on Lua's locale-dependent ordering.
- File lock waits (file cache, local storage) poll with a jittered backoff instead
  of blocking in `flock`, so a canceled waiter behind a paused holder no longer
  keeps a goroutine, an OS thread and a file descriptor until the holder releases.
- Idempotency migration `000002_index_retained_outcomes` first drops a leftover
  (possibly INVALID) `foundry_idempotency_retained` index, drops the old caller
  index only `IF EXISTS`, and raises the session `lock_timeout` to 300 seconds for
  the concurrent build before restoring the runner's value. The idempotency guide
  documents recovery from a failed build.
- Adapter types declared outside the framework that embed a framework adapter are
  no longer trusted as framework-owned by the inherited marker method; their own
  methods stay isolated, so `runtime.Goexit` in them is contained. Lease
  acquisition and restore release their admission slot even if an adapter exits
  the goroutine.
- `Topic.SubscribeKeys` on a zero `Topic` returns `fault.Invalid` instead of
  panicking.
- A maintenance store that uses the `null` cache driver is rejected at validation,
  since it would silently discard a fleet-wide pause.
- Redis tagged reads no longer delete a current payload that only exceeds the
  reader's `MaxValueBytes`; it is a miss, and processes with different bounds no
  longer delete each other's valid entries. Obsolete or malformed payloads are still
  reclaimed.

- A receive that is waiting when delivery is interrupted (overflow, disconnect
  or an undecodable payload) now reports that terminal cause instead of the
  cancellation of its internal operation, so `Supervise` reports the real gap
  cause. An explicit `Close` or broker shutdown still reports cancellation.

#### Changed

- The Redis client, lease manager and cache fills now wait briefly for capacity
  instead of failing immediately. A full Redis `MaxOperations` or lease `MaxActive`
  queues callers in FIFO order for at most the operation timeout (capped at five
  seconds) and the caller's deadline, then returns retryable `fault.Overloaded`
  (HTTP 503 with `Retry-After`); `Close` ends queued waits with `fault.Closed`. A
  full cache `MaxFills` loads directly without coalescing rather than failing, and a
  full `MaxFillWaiters` returns `fault.Overloaded` instead of `fault.Conflict`.
- Redis defaults rose to 64 pooled connections (`MaxConnections`) and 1024 admitted
  operations (`MaxOperations`); the per-command admission path uses atomics and a
  semaphore instead of a shared client mutex.
- Redis tagged payloads store their concatenated snapshot versions as a fingerprint
  compared server-side. Tagged payloads written by earlier releases are obsolete
  misses that the next write replaces.
- Fixed rate-limit windows now start at a deterministic per-key phase
  (`ratelimit.Key.WindowOffset`) instead of Unix-epoch multiples, so many keys no
  longer reset at the same moment. `ResetAfter` stays within the window.
- A busy rate-limit store, pub/sub broker, typed Redis data store or raw Redis
  command store now waits briefly for a slot, then returns retryable
  `fault.Overloaded` (HTTP 503 with `Retry-After`) instead of failing immediately
  with `fault.Conflict`. A pub/sub subscription limit fails
  immediately with `fault.Overloaded` (subscriptions are long-lived, so they do not
  queue).
- All typed Redis pub/sub subscriptions of one client now share one dedicated
  connection. A channel is unsubscribed only when no remaining subscription needs
  it, the connection closes with its last subscription, and a lost connection fails
  every subscription on it. `Publish` still returns Redis's subscriber count, which
  counts connections, so several subscriptions of one client count once.
- The memory cache evicts in recency order with a second chance for entries read
  since they were last ordered (approximately LRU) instead of strict LRU; reads no
  longer reorder storage.
- Exhausted local idempotency admission (`MaxActive`) queues briefly, then returns
  retryable `Unavailable` (HTTP 503 with `Retry-After`) wrapping `fault.Overloaded`,
  instead of the caller-quota `Capacity` (429). `idempotency.Store.Done` closes
  after both active operations and the pruner have exited.
- PostgreSQL cache reads are single statements without the schema-wide advisory
  lock or an explicit transaction. `Put`, `Add` and `Forget` are single statements;
  `Increment` and `Expire` lock only their own row.
- File cache mutations serialize per key through 32 hash-sharded process locks
  instead of one root lock polled every 10ms. Capacity uses a backend-owned usage
  estimate that each prune pass recounts, instead of listing the directory or
  counting the table on every write. Replacement checks only the record header.
- The PostgreSQL cache now has `Start`/`Close`/`Done`. Configured applications start
  and drain it with the application; `Start` performs no I/O.

#### Added

- `cache.Config.LoadTimeout` (default 30 seconds, at most `cache.MaxLoadTimeout` of
  24 hours) bounds each `Remember` loader, and `cache.WithLoadTimeout(d)` narrows one
  call.
- `Cache.GetMany(ctx, keys...)` returns `[]cache.Lookup[V]` in input order under one
  tag snapshot; memory and Redis (one script) implement the optional
  `cache.BatchReadBackend`/`TaggedBatchReadBackend` capabilities, and other adapters
  read each distinct key. `Cache.Pull` reads and then forgets one entry.
- `Cache.PutMany(ctx, ttl, entries...)` writes up to `MaxBatchEntries` typed
  `cache.Entry[K, V]` pairs after encoding all of them, under one snapshot, in
  input order (not atomic; the first failure stops the batch).
- `cache.WithObserver` installs a synchronous `cache.Observer` that receives a
  `cache.Event` per completed typed call (family, `cache.Operation`, key hits and
  misses, Remember `Loaded`/`Unpublished`, duration and failure `fault.Code`),
  never keys, values or error text.
- `Cache.Flexible(ctx, key, fresh, stale, loader)` serves a value younger than
  `fresh` directly and an older one (up to `fresh+stale`) immediately while one
  coalesced store-owned background fill refreshes it (stale-while-revalidate).
  Freshness is a reserved marker entry with TTL `fresh`, read with the value in one
  batch; it counts toward adapter capacity, invalidation removes it, and plain
  writes leave it unchanged.
- `cache.WithMemo(ctx)` memoizes typed reads (`Get`, `GetMany`, `Exists`,
  `Remember`) for the context's lifetime; writes and invalidations through the
  context forget the affected results. At most 256 results and 1 MiB are memoized.
- `cache/null` supplies a backend that retains nothing but implements every cache
  capability, and configured applications can select it with the `null` cache
  driver to disable a store without changing declarations.
- `Store.Stats()` returns lock-free totals of hits, misses, writes, publication
  failures, loader runs, coalesced followers, uncoalesced loads and snapshot
  re-resolutions. `cache.WithLogger` supplies the logger for failures that do not
  fail the caller.
- `cache.FlushBackend` lets adapters without tags implement `Store.Invalidate` by
  physically removing the namespace's entries; the file and PostgreSQL caches
  implement it. `cache.SnapshotReadBackend` lets an adapter resolve tag metadata and
  read an entry in one round trip (Redis implements it).
- File and PostgreSQL cache stores run an owned background pruner while started
  (`prune_interval`, default one minute; `0` disables it). Failures are logged as
  redacted diagnostics. `Sweep(ctx, limit)` reports removed, corrupt and
  unrecognized records and the recounted usage.
- `Leases.ForceRelease(ctx, key)` removes a lease whatever its owner (memory and Redis
  implement the optional `lease.ForceBackend`). `Leases.Export(guard)` and
  `Leases.Restore(ctx, token)` hand an explicit guard to another process as a typed,
  redacted `lease.Token[K]`; the exported guard ends with `lease.ErrExported` without
  releasing the key. Tokens are single-use: `Restore` atomically swaps the owner
  secret for a fresh one (`lease.TransferBackend`, implemented by memory and
  Redis), so restoring a redelivered token returns `lease.ErrLost`. `lease.DefineSemaphore(name, codec, slots)` bounds concurrent
  holders of each typed resource key across processes. `Manager.PollInterval`
  exposes the configured contention poll interval.
- `Limiter.Peek`, `Remaining`, `AvailableIn`, `Clear` and `Attempt`, backed by the
  optional `ratelimit.InspectBackend` capability that memory and Redis implement.
  `Clear` also repairs unreadable Redis state.
- `Topic.SubscribeKeys(ctx, first, rest...)` subscribes to several keys of one topic;
  each `pubsub.Delivery` carries the key it came from. `Topic.Supervise` keeps one
  key subscribed across dropped connections with jittered exponential backoff
  (`pubsub.SupervisePolicy`) and calls a gap handler with `pubsub.Gap` before
  delivering further messages, so applications can catch up on what they missed.
- Typed Redis data: `data.Hash.GetMany` (one script, `value.Optional[V]` per
  requested field) and `GetAll` (requires `HashDeclaration.WithFieldDecoder`),
  `data.IncrementField` for `Hash[K, F, int64]`, bounded sorted sets
  (`data.DefineSortedSet`: `Add`, `Increment`, `Remove`, `Score`, `Rank`, `Range`,
  `RangeByScore`, `CountByScore` with `data.Window`/`data.ScoreRange`) and bounded
  lists (`data.DefineList`: `Push`, `PushFront`, `PopFront`, `PopBack`, `Range`,
  `Trim`). Adapters opt in through the new `HashReadBackend`, `HashCounterBackend`,
  `SortedSetBackend` and `ListBackend` capabilities; Redis implements all of them.
- `idempotency.Config.PruneInterval` (default ten minutes; zero disables) and
  `PruneBatch` (default 500, at most `idempotency.MaxPrune`) schedule a store-owned
  pruner. The application module starts it with the application logger; standalone
  stores call `Store.Start(ctx)`. `idempotency.WithLogger` supplies the logger for
  redacted pruning failures, and `Close` stops the pruner.
- Migration `000002_index_retained_outcomes` (`idempotency.IndexRetainedOutcomes`)
  replaces the caller index with `(namespace, scope_digest, expires_at)` using
  nontransactional `CREATE/DROP INDEX CONCURRENTLY`.

#### Performance

- Redis typed cache reads resolve or create namespace/tag versions and read the
  tagged entry in one atomic script instead of separate round trips; the store
  derives its namespace stamp address once at construction.
- The memory cache reclaims expired entries earliest-deadline-first from an expiry
  heap instead of sweeping the table, copies hit bytes after releasing its lock and
  no longer re-inserts unchanged tag metadata on every resolution. Reads (`Get`,
  tagged and batch reads, `Exists` and resolution of existing tag metadata) share a
  read lock and mark entries referenced instead of taking the exclusive lock.
- Redis typed `Put`, `Add`, `Forget`, counter `Increment` and `Expire` resolve or
  create namespace/tag versions and mutate in one script (the new optional
  `cache.SnapshotWriteBackend` capability), so each costs one round trip instead of
  two and can no longer fail with `Conflict` because an invalidation ran between
  resolving and writing. A Remember miss costs three round trips instead of four.
- A waiting `lease.Semaphore` acquisition tries every slot once, then at most eight
  random slots per poll instead of every slot on every poll.
- The memory lease backend reclaims expired owners from an expiry heap in deadline
  order before checking capacity instead of scanning every entry on each operation.
- The memory rate limiter removes expired buckets from an expiry-ordered heap in
  amortized logarithmic time instead of scanning the table under its mutex for each
  new key when full.
- A new idempotent operation issues 13 SQL commands instead of 22, and a replay 7
  instead of 12. Infrastructure statements set transaction-local settings in one
  round trip instead of opening savepoint and `search_path` scopes, pruning uses one
  schema-qualified statement, and the operation runner no longer starts a goroutine
  per call.
- PostgreSQL cache writes no longer run `count(*)`/`sum` over the whole table, and
  reads no longer serialize on a schema-wide lock. File cache writes no longer list
  the directory or read and hash the previous record.

### Jobs, scheduling, events, outbox, mail and notifications

#### Fixed

- A job handler that returned nil now always succeeds, even if the worker began
  stopping or the job deadline passed after its side effects; `PreventRetry`
  with nil is also a success. Only non-nil errors are classified by cause.
- Worker shutdown is a graceful drain: reservations stop while admitted handlers
  keep their heartbeats and run for up to `WorkerConfig.DrainTimeout` (default
  5s) before cancellation. An attempt interrupted by `Stop` or the drain deadline
  is released with a refund (`jobs.Result.Refund`), so it no longer consumes its
  retry budget on the memory and Redis backends. The scheduler drains running
  tasks the same way (`schedule.Config.DrainTimeout`), keeping leadership and
  overlap leases. Application assembly rejects drain budgets that do not fit
  within `ShutdownTimeout - StopDelay`.
- Queue capacity (`QueueConfig.MaxEntries`/`MaxBytes`) now counts only live
  jobs. Terminal records are retained separately up to `MaxRetained` (default
  65536) and evicted oldest-first, so retained successes and failures never make
  enqueue fail. A full queue returns the retryable `jobs.ErrQueueFull`
  (`fault.Overloaded`, HTTP 503) instead of `fault.Conflict`. Redis now reports
  an identity conflict (`Conflict`), a full queue (`Overloaded`) and a queue
  policy mismatch between processes (`jobs.ErrQueuePolicy`, `fault.Internal`)
  distinctly.
- Worker backend failures (reserve, renew, start, finish, including Redis client
  overload) no longer stop the worker. Each is logged as `job backend operation
  failed` with a redacted diagnostic, and the loop backs off with jitter up to
  `WorkerConfig.FailureBackoff`. A renewal error keeps retrying until the lease
  would really have expired, and a lost lease cancels only its own handler.
- An unknown job name/version at the worker is transient by default
  (`WorkerConfig.RetryUnregistered`): it retries with the envelope's backoff, as
  during a rolling deploy, before failing as `unregistered`. A reservation that
  repeatedly expires before its attempt starts (a payload that crashes its
  process) now fails as `delivery_limit` instead of being redelivered forever.
- Retry jitter is proportional to the delay (`[0, max(Jitter, delay/5)]`), so
  failures with long backoff no longer retry in lockstep.
- The outbox publisher retries with exponential backoff and jitter (1s doubling
  to 5 minutes, 1,000 attempts: about 83 hours of broker outage) instead of a
  fixed delay. `Run` no longer returns on transient SQL, transaction or
  publication errors: it logs a redacted diagnostic through the application
  logger, backs off and continues. `fault.Missing` (a job or event not yet
  registered in the publishing process) is transient instead of permanent.
  Observer failures are logged and never stop `Run`.
- Scheduler catch-up resumes after a per-schedule cursor of the last completed or
  skipped occurrence, persisted in Redis coordination (`schedule.CursorBackend`),
  so restarts and leadership changes no longer replay completed occurrences. A
  backlog beyond `CatchUp.Max` now runs the most recent occurrences instead of
  the oldest. A due occurrence that finds every slot busy waits up to
  `Config.CapacityWait` (default 5s) before it is skipped as `capacity_reached`;
  default `Concurrency` is 16. Skipped occurrences and failed invocations are
  logged with redacted diagnostics and observed with `EndWithDiagnostic`.
- Email: a DNS, dial or TLS-handshake failure before a provider connection was
  obtained is `Transient` (retried) instead of `Ambiguous`. SMTP authentication
  that the server does not advertise or rejects with 5xx (including 504) is
  `Permanent`; 4xx stays `Transient`. The SMTP EHLO name defaults to the host
  name, never `localhost`. MIME `Message-ID` is derived from the delivery's
  idempotency key, so retries of one queued email carry the same ID, and its
  domain is configurable (`email.Config.MessageIDDomain`, default: the sender's
  domain) instead of `foundry.invalid`.
- Notifications captured before a binding's channel set changed stay readable
  and deliverable with the channels they were captured with; adding, removing or
  retiring a channel no longer makes older notifications invalid. A realtime
  publication that provably failed before anything was published (local
  admission, a stopping hub, encoding; `websocket.NotPublished`) is retried
  instead of becoming terminal `Uncertain`; a failure after publication began
  stays `Uncertain`.
- The process-local memory job driver is rejected outside the `local`,
  `development`, `dev`, `test` and `testing` environments unless the connection
  sets `allow_memory = true`, because jobs dispatched by a process without a
  worker would be silently lost.
- Redis workflows with a completion, catch and finally job at the maximum size
  (`jobs.MaxWorkflowSteps` steps, `jobs.MaxWorkflowMembers` jobs in all) run to
  completion: record positions are validated against all members, not only the
  steps, and larger workflows are rejected before reaching the backend.
- `jobs migrate-layout` counts retained failures from each record's own state:
  a record that failed, was retried and then succeeded is no longer counted as
  failed, and finished workflow groups' bytes are accounted as retained.
- The outbox publisher gives each publication its own deadline within
  `OperationTimeout`, keeping a bookkeeping margin for the batch transaction: a
  slow route no longer rolls back the batch and loses the attempts of rows that
  were published or timed out; those rows record their attempt and back off.
- A worker whose attempt start fails while it is stopping refunds the attempt,
  because the start may have applied before its reply was lost.
- A Redis queue policy mismatch between processes (`jobs.ErrQueuePolicy`,
  `fault.Internal`) is transient for the outbox publisher instead of permanent;
  fix the configuration and the rows publish.
- A finished workflow's group metadata bytes move from live to retained
  accounting (memory and Redis), so completed workflows no longer consume live
  queue capacity until retirement.
- Enqueues wake only workers subscribed to the same queue
  (`jobs.WakeBackend.JobWakeup(Key)`); workers wait on each of their queues.
- `events.Bus.Intercept` requires a framework-internal token and is reachable
  only through `testkit/events` fakes.
- `notifications.Binding.EnqueueMany` reports every ID of a failed run
  (`BulkResult.Stored`, `Unconfirmed`, `NotAttempted`) and
  `EnqueueManyWithIDs` accepts caller-supplied IDs, so an uncertain batch can be
  reconciled or retried idempotently.
- Failed-job archive, outbox and notification inbox pruning select each batch
  with one bounded `DELETE … WHERE id IN (SELECT id … LIMIT n)` and no longer
  load payloads.
- The failed-job archive stores each envelope's original bytes
  (`000002_store_original_envelopes`), bounded by `jobs.MaxEnvelopeBytes`
  before writing; jsonb re-rendering no longer rejects valid envelopes or makes
  near-limit ones unretryable.
- Failed-job archive listing pages by `(failed_at, id)` with an
  `archive.Cursor` (`Page.Next`, `ListOptions.After`, `failed-jobs list
  --after cursor`), so entries sharing an instant are not skipped and a pruned
  last entry does not restart the listing. `notifications.Manager.Deliveries`
  returns `notifications.ErrDeliveryAnchorMoved` when its `after` delivery left
  the listed state instead of silently restarting.

#### Changed

- Burst admission in the job dispatcher (default `MaxInFlight` 256), event bus
  (256), mailer (`MaxActive` 64), notification manager (`MaxActive` 128) and
  outbox publisher now waits briefly for a slot and then fails with the
  retryable `fault.Overloaded` instead of failing immediately with
  `fault.Conflict`. A nested operation from an active one never waits.
- Redis job queues use storage layout 2: the immutable envelope is stored apart
  from small mutable state, and the queue policy identity now includes
  `MaxRetained`. Existing layout-1 queues are never migrated implicitly: every
  operation on one returns the typed `jobs.ErrLegacyLayout` and changes nothing,
  so the previous release keeps working. After stopping or draining the previous
  release, run `jobs migrate-layout --queue … --confirm`
  (`Dispatcher.MigrateLayout`, `redis.JobBackend.JobMigrateLayout`), which
  migrates atomically, keeps every record and is idempotent. Rollback needs a
  pre-migration Redis snapshot or a fully drained queue (see the jobs operations
  guide and the compatibility table).
- Idle worker loops double their wait from `PollInterval` up to
  `MaxPollInterval` (default 1s) and wake immediately on an enqueue, workflow or
  manual retry accepted by the same backend instance. Each cycle tries every
  distinct subscribed queue at most once.
- The outbox publisher runs only beside the kernels listed in
  `features.outbox.kernels` when set (recommended: `worker` or `scheduler`);
  `foundation.Runtime.SelectedKernels` reports the kernels a process runs.

#### Added

- Queue operations: `Dispatcher.Stats`, `RetryFailed`, `FlushFailed`, `Forget`
  and `Clear` (optional `jobs.StatsBackend`/`ForgetBackend`, implemented by the
  memory and Redis backends) and the `jobs stats|retry-failed|flush-failed|forget|clear`
  commands. Bulk commands page through the queue and require `--confirm`.
- Outbox operations: `Publisher.Stats`, `Failed`, `Requeue` (all, by ID or by
  producer kind) and `Prune` (published rows older than a cutoff, bounded
  batches), the `outbox stats|failed|requeue|prune` command, and migration
  `000003_add_claim_indexes` with partial indexes for the pending claim and for
  pruning.
- Notification operations: `Manager.Deliveries`, `ResolveDelivery` (delivered,
  rejected or resend, guarded by the observed state) and `Deliver`, the
  `notifications deliveries|resolve|deliver` command, and inbox `MarkAllRead`
  and `Delete`.
- SMTP `AUTH LOGIN` and `XOAUTH2` (`smtp.Config.Auth`), a configurable EHLO name
  (`LocalName`) and an optional connection pool (`MaxIdle`, `IdleTimeout`, reset
  with `RSET`; configured mailers default to 2 idle connections).
- `email/failover` composes transports: `failover.New` tries them in order and
  `failover.RoundRobin` rotates the starting transport; both move on only after
  a known non-acceptance. Configured mailers use `driver = "failover"` or
  `"roundrobin"` with `transports = [...]`.
- Scheduler helpers: `EveryMinute`, `EveryFiveMinutes`, `EveryTenMinutes`,
  `EveryFifteenMinutes`, `EveryThirtyMinutes`, `LastDayOfMonthAt`, and options
  `Days` (`Weekdays()`, `Weekends()`), `Between`/`UnlessBetween` windows,
  `LastDayOfMonth`, the `When` predicate (skips as `filtered`) and
  `EvenInMaintenanceMode`. `Scheduler.RunNow` and the `schedule test --id`
  command run one schedule immediately.
- `HandlerOptions.Skip` completes a job successfully without running middleware
  or the handler when the work became unnecessary.
- Test helpers: `testkit/jobs.AssertPushed`, `AssertNotPushed` and
  `AssertPushedCount` with typed payload predicates; `testkit/email.AssertSent`,
  `AssertNotSent` and `AssertSentCount`; and `testkit/notifications`, a
  recording custom-channel transport with typed delivery assertions.

- Job execution policies: `HandlerOptions.Overlap = jobs.WithoutOverlapping(leases,
  key, options)` holds a typed, owner-conditional lease from before an attempt
  starts until the handler actually exits (renewed while it runs; losing it
  cancels the handler) and releases an overlapping job for a delay without
  consuming an attempt, or skips it. `HandlerOptions.Throttle =
  jobs.ThrottleExceptions(limiter, key, backoff)` uses a typed rate limiter as an
  exception budget and releases jobs while it is exhausted. `Policy.MaxExceptions`
  fails a job with reason `exception_limit` after that many handler exceptions
  (backends count `Record.Exceptions`), `Options.RetryUntil`/`Policy.RetryUntil`
  stops retries and starts after an absolute deadline (reason `retry_expired`),
  and `Unique.UntilProcessing` releases the uniqueness window when the first
  attempt starts. These fields use envelope format 3 (`jobs.ExtendedEnvelope`),
  selected only when used; upgrade workers and publishers before dispatching them.
- The `sync` job driver (`jobs/inline`): its dispatcher runs each accepted job
  inline in the caller through the worker path (admission, middleware, retries,
  history), with `Dispatcher.RunPending` for due retries. It is non-durable and
  follows the memory driver's environment guard.
- Workflow `WithCatch` and `WithFinally` callback jobs, released once every member
  is terminal (catch only after a failure; an untriggered catch ends with reason
  `not_triggered`); `Workflow.Status`/`Dispatcher.WorkflowStatus` progress
  (`jobs.WorkflowStatus`); and `Workflow.Enqueue` for transactional workflow
  enqueue through the job outbox.
- Queued event listeners: `events.Listen(...).Queued()`, `RegisterQueuedListener`
  and `application.ListenQueued` run a listener as a job through a per-bus
  `events.ListenerQueue`, mixed with sync listeners in declaration order
  (`features.events.queued_listeners`). Event subscribers (`events.Subscriber`,
  `events.Subscribe`, `RegisterSubscriber`, `application.Subscribe`) group several
  listeners in one type. `testkit/events.NewFake` suppresses a bus's listeners and
  records payloads for typed `AssertDispatched`/`AssertNotDispatched`/
  `AssertDispatchedCount` assertions.
- Mail: `TemplateSource.Layout` wraps text and HTML bodies with a typed layout;
  `email.NewLocalizedTemplate[T]` selects a template along an `i18n.LocaleSet`
  fallback chain and records the locale on the message (`Message.Locale`, sent as
  `Content-Language`); `email.NewDataAttachment` and `Message.AttachData` attach
  bounded in-memory content; `log.NewPreview` (`driver = "preview"`) logs the
  rendered message for development with credential-like headers redacted.
- Notifications: `notifications.BindOnDemand` with `notifications.Route` delivers
  to explicit email/custom addresses without a recipient model;
  `Binding.EnqueueMany` notifies up to 10,000 recipients in bounded batches with
  one storage-and-outbox transaction per batch; `Manager.PruneInbox` deletes old
  inbox records in bounded batches (migration `000002_add_inbox_prune_index`).
- The optional durable failed-job archive (`jobs/archive`): workers record every
  confirmed terminal failure (complete envelope, reason, attempts) through a
  `jobs.FailureSink` (`jobs.WithFailureSink`, `jobs.RegisterFailureSink`, or
  `worker.archive` settings) into `foundry_failed_jobs`; `Store.List`,
  `Store.Retry` (re-dispatch as a new job via `Dispatcher.Redispatch`) and
  `Store.Prune`, with the `failed-jobs list|retry|prune` command.
- Queue depth metrics: `jobs.DepthMonitor` samples `Dispatcher.Stats` in the
  background; configured applications with observability export
  `foundry_jobs_queue_jobs{connection,queue,state}` and
  `foundry_jobs_queue_sampled_timestamp_seconds` through the `foundry.jobs`
  collector.
- Opt-in encrypted job payloads: `definition.Encrypted(keyring)` seals payloads at
  capture and workers decrypt them before decoding; `Definition.Payload` decodes
  and decrypts an envelope for tooling.

#### Performance

- Redis job heartbeats and transitions no longer decode or rewrite the job
  envelope and history payload, and an empty reservation writes nothing.
- The outbox publisher claims up to `MaxInFlight` rows across all routes in one
  transaction and publishes them concurrently, with adaptive idle polling,
  instead of one transaction per route per row.
- Provider API mail drivers (Resend, Postmark, Mailgun) skip MIME rendering;
  pooled SMTP connections avoid a TCP/TLS/AUTH handshake per message.
- The final notification status report reads only delivery rows instead of
  reloading and re-verifying the envelope.

### Storage, attachments, imaging, HTTP client and webhooks

#### Fixed

- Attachment cleanup now works on versioned buckets. A retained version ID is
  deleted by `Version` alone (exactly that version, no delete marker) instead of
  `IfMatch`+`Version`, which AWS rejects as `Unsupported`; previously every
  replace, detach and owner delete failed there and orphans accumulated.
  `storage.Capabilities` gains the combination flag `ConditionalVersionDelete`
  plus `ValidatePut`/`ValidateRead`/`ValidateDelete`/`ValidateList`, the shared
  checks used by `Disk` and the adapters.
- The default image budget now admits the advertised 25-megapixel `Pixels` limit.
  `WorkingBytes` is checked per pipeline phase with measured per-pixel costs
  (decoder, transforms, encoder) instead of `input*4 + pixels*64 + output`, which
  capped images at about 7 MP and rejected ordinary 12 MP phone photos. The
  default rises from 512 MiB to 640 MiB; the imaging guide documents the per-phase
  costs and memory per concurrent operation. Full-size 25 MP lossless WebP output
  (about 1.5 GiB) still requires a larger budget.
- Attachment reads (`Load`, `List`, `First`, `Find`, `ReadBytes`, `Image`, links,
  inspection) and writes use separate queued admission (`MaxReads` 64,
  `MaxActive` 8, previously 2 shared), and owner-delete observers use their own
  pool, so a model deletion no longer fails because uploads are busy. Exhausted
  capacity waits briefly, then fails as retryable `fault.Overloaded`.
- S3 listing no longer sends a HEAD per listed object. Entries come from the
  `ListObjectsV2` page (content type and checksum stay zero; use `Stat`), and a
  foreign, oversized, unparsable or unvalidated entry is counted in the new
  `Page.Skipped` instead of failing the whole page.
- Open storage readers no longer hold the 5-minute operation timeout and one of
  the 32 operation slots for the whole download. Streams use a separate
  `Config.MaxStreams` pool (default 256) and are cancelled only after
  `StreamIdleTimeout` (default 2 minutes) without read progress, so slow clients
  are not cut off mid-body and cannot block `Put`/`Stat`. Disk and S3 upload
  admission queue briefly and then report `fault.Overloaded`. The HTTP storage
  bridge maps capacity exhaustion, disk shutdown and server deadlines to 503
  `unavailable` instead of 500/408.
- A failed or refused verification HEAD after S3 `PutObject` or
  `CompleteMultipartUpload` (for example with write-only credentials) no longer
  reports an applied publication as a failure, which left attachments
  `uncertain`. The acknowledgement's ETag/version is returned; the pinned HEAD is
  optional (`s3.Config.VerifyPublication`, default on) and only refines
  Last-Modified.
- Empty outbound HTTP request bodies are sent as `http.NoBody`: no chunked empty
  body (411 from strict servers) and no body-probe goroutine per GET. Requests
  carrying an idempotency key keep the replay guard.
- Webhook verification work is bounded: duplicate signatures count once, at most
  eight distinct signatures and eight Ed25519 checks are considered, each HMAC
  key is computed once, and verification stops at the first match. Standard
  Webhooks delivery IDs accept the visible ASCII provider IDs the specification
  allows (except `.` and `,`).
- Foundry cloud credentials without a reported expiry are now refreshed every
  five minutes instead of being cached forever by the SDK, so rotating providers
  take effect; the synthetic refresh deadline does not shorten signed URLs.
- The S3 adapter no longer installs a no-retry policy for the whole AWS config,
  which also disabled retries of IMDS/STS/SSO credential clients; only S3 API
  operations are single-attempt. Replayable-operation retryers are built once
  per backend instead of per call.
- `httpclient.Module` snapshots destination host/network and retry-status slices
  when it is called, not later during provider construction.
- Attachment display names strip zero-width and bidirectional formatting
  characters (for example U+202E), so a name cannot render as a different
  extension; `internal/filename.Normalize` (download `Content-Disposition`)
  applies the same rule.
- Outbound HTTP capacity exhaustion is a `*httpclient.Error` of kind `Overloaded`
  (still matching `fault.Overloaded`).
- Public storage URLs encode a literal `+` in keys as `%2B`, which some CDNs and
  S3-compatible servers otherwise decode as a space.
- The HTTP client fake records a request only after reading its complete body,
  instead of exposing a blank placeholder while the body is consumed.

- HMAC webhook presets (GitHub, Shopify and custom schemes) no longer take the
  delivery ID from an unsigned provider header: a captured body and signature
  could be replayed forever with a fresh `X-GitHub-Delivery`, and every replay
  passed durable idempotency. `Delivery.ID` is now `sha256-` of the verified
  signed content (body, plus the signed timestamp when present); the provider
  header is exposed only as `Delivery.UnverifiedProviderID`.
- S3-compatible disks require explicit static credentials, as R2 does. Without
  them, or with a chain credential source, the AWS default chain could send the
  host's AWS machine credentials (environment, IMDS, ECS, SSO) to a MinIO,
  Spaces or B2 operator.
- `outbound.Queue.ReplayFailed` skips deliveries of inactive endpoints instead
  of rolling back the whole batch, and reports committed replays only.
- Outbound deliveries no longer stay `pending` forever after pre-send failures
  (storage, signing secrets) or runtime-decided exhaustion (retry deadline,
  exception limit, timeout). Each attempt is logged with a category, and the new
  `DeliveryJob.FailureSink` records terminal job failures as `failed`; the
  handler no longer re-derives finality from the declared attempts.
- An accepted webhook (2xx) whose response body exceeds the client's response
  limit is recorded as delivered, not retried and then failed. Responses are
  read through a bounded stream and never buffered.
- `outbound.Config.SecretGrace` is used: `RotateSecret(ctx, endpoint)` keeps
  previous secrets for the configured grace, and `RotateSecretWithGrace` takes
  an explicit overlap.
- Server-side copies (S3 `CopyObject`, local file copy) check the source size
  against both disks' `MaxObjectBytes` before publishing, failing with
  `LimitExceeded`/`Unchanged`, so an oversized move publishes and deletes
  nothing. S3 copies always use `MetadataDirective: REPLACE` and write the same
  metadata as a streamed copy instead of inheriting the source's headers.
  `ServerCopier.Copy` takes the maximum size.
- Presigned upload links reject a zero `Size`: SigV4 does not sign a zero
  Content-Length, so an empty link accepted uploads up to 5 GiB.
- Local conditional publications and conditional deletes hold their shard lock
  until the directory sync made the change durable.
- Attachment links never render script-capable files (SVG, HTML, XML,
  JavaScript) inline by default: `PublicURL`/`PublicURLOf` fail with
  `ActiveContentRefused`, and signed links force a download disposition unless
  `Policy.InlineActiveContent` is set.

#### Changed

- Attachment media detection recognizes OOXML (docx/xlsx/pptx and
  macro-enabled variants), ODF/EPUB, SVG and ISO media brands (AVIF, HEIC/HEIF,
  MP4, QuickTime, 3GPP, M4A), so `Accepted` lists for these work. The client
  content-type hint only specializes plain text to CSV/TSV/Markdown/calendar or
  valid JSON when the policy accepts it; it never overrides binary detection.
- `attachments.DefaultConfig()` now admits 8 writes and 64 reads (previously 2
  operations in total).
- `ReconcilePending` settles `writing`/`uncertain` intents automatically once they
  are older than the longest disk `Timeout` plus `Config.SettleAfter` (default 15
  minutes), and only when storage is unambiguous: absent, or present with the
  intent's exact size and full checksum. Ambiguous intents stay `uncertain` with
  failure `settlement_ambiguous`; `Settle` remains the operator path.
- Attachment `PublicURL`/`TemporaryURL` no longer run a storage HEAD; keys are
  immutable per upload, and membership is still rechecked.
- The local store locks per key hash shard (holding the store lock shared, so
  older processes that take it exclusively still exclude writers), waits in the
  kernel instead of polling every 10 ms, releases the lock before the staging
  close and directory sync, and syncs parent directories only when a shard is
  first created. `internal/filelock` gains `AcquireShared`; `Acquire`/`Try` keep
  their API for `cache/file`.
- `httpclient.DefaultRetryPolicy()` enables full jitter.

#### Added

- `storage.PutOptions.Metadata`: Cache-Control, Content-Disposition,
  Content-Encoding, storage class, SSE-KMS key ID and custom metadata
  (`Capabilities.ObjectMetadata`, S3 profiles).
- Presigned direct uploads: `Disk.TemporaryUploadURL` returns a redacted
  `storage.UploadLink` whose exact size, media type, checksum metadata and
  optional absence condition are signed (S3 PUT, up to 5 GiB). Presigned reads
  accept `LinkOptions.ResponseContentType`/`ResponseContentDisposition`.
- Directory and batch APIs: `ListOptions.Delimited` with `Page.Directories`
  (local and S3), `Disk.DeleteMany` (one `DeleteObjects` request on AWS) and
  `Disk.DeletePrefix`.
- Server-side copies: `storage.ServerCopier`; `CopyTo`/`MoveTo` between disks on
  the same adapter use S3 `CopyObject` (pinned to the source ETag/version) or a
  local file-to-file copy instead of streaming through the process.
- `s3.Compatible` provider (`CompatibleConfig`, `CompatibleCapabilities`) for
  MinIO/Spaces/B2 with explicitly declared capabilities, `AllowHTTP` opt-in for
  local plain-HTTP endpoints, and the `s3_compatible` disk driver.
- `s3.Config.PartConcurrency` (bounded concurrent multipart parts) and
  `VerifyPublication`; `storage.Config.MaxStreams`/`StreamIdleTimeout`;
  `Disk.Config()` and `Stats.Streams`.
- Attachment `Open` (streaming, digest-verified reads of any stored size),
  `PublicURLOf`, `PublicURLsOf` and `TemporaryURLOf` (links from loaded rows with
  no database or storage I/O), and `Config.MaxReads`/`SettleAfter`.
- `Collection.AddFromURL` imports a remote file through an `httpclient.Client`
  that enforces a restricted destination policy (`Client.RestrictsDestinations`),
  within the collection's byte limit and ordinary media detection.
- Imaging `Plan.Background` (flatten transparency before JPEG) and
  `Plan.AVIFQuality`.
- HTTP client: `Retry-After` handling, `RetryPolicy.Jitter` and `Statuses`,
  `Request.Form`, `Multipart` (`Field`/`File`), `BasicAuth`, `Config.TLS`
  (custom CAs, client certificates, server name), `Client.Download` (streams to a
  writer beyond `ResponseBytes`) and `Client.DoAll` (bounded ordered batch).
  `testkit/httpclient` gains `NewRoutes`/`On` URL-pattern fakes, `Unmatched`,
  `AssertSent`, `AssertNotSent` and `AssertSentCount`.
- Webhooks: `webhook.HMAC` protocol with a configurable `HMACScheme` (header,
  prefix, SHA-256/512/legacy SHA-1, hex/base64, optional signed timestamp and
  delivery header) and `GitHubConfig`, `ShopifyConfig` and `SlackConfig` presets.

- Attachment image variants: `attachments.DefineVariant(name, plan)` and
  `Policy.Variants` declare named derived images (for example thumbnails)
  stored beside the unchanged original in the new `foundry_attachment_variants`
  journal (migration `000002_create_variants`, generated
  `internal/attachmentstore.Variant`). Variants are generated after publication,
  or through the typed `DefineVariantJob`/`WithVariantQueue` outbox job
  (`Manager.GenerateVariants` is idempotent); `Result.PendingVariants` reports
  queued or failed generation without unpublishing. Variants follow the
  original's lifecycle (replace, detach, clear, owner deletion, orphan pruning
  and `DetachKeepFile`), are deleted before it, and are swept by
  `ReconcilePending`. Loaded attachments carry them (`Attachment.Variant`,
  `Variants`); `VariantPublicURLOf`/`VariantTemporaryURLOf` link without I/O and
  report `VariantUnavailable` until generated. `RegenerateVariants` (collection
  and manager forms) processes bounded batches, and the `attachment-variants`
  command (`command.VariantDeclaration`) runs them.
- Browser form uploads: `Disk.TemporaryUploadForm` returns a redacted
  `storage.UploadForm` for an S3 POST policy pinned to the exact key, exact
  Content-Type, a `content-length-range` and optional checksum metadata. R2 does
  not support it; compatible profiles declare `CompatibleCapabilities.FormUploads`
  (`cloud.compatible.form_uploads`).
- Outbound webhooks: `webhook/outbound` sends typed events
  (`DefineEvent` over a generated JSON contract) to a registry of endpoints
  whose URLs must pass the restricted `httpclient` destination policy. Each
  endpoint's signing secrets are stored encrypted with the application keyring
  and rotated with a grace period (`RotateSecret`). `Send`/`Publish` write a
  delivery log row and enqueue a typed `DefineDeliveryJob` job in the caller's
  transaction; attempts carry Standard Webhooks `webhook-id` (stable across
  retries and replays), `webhook-timestamp` and multi-secret `webhook-signature`
  headers verifiable by `webhook.Standard`. Retryable failures follow the job
  backoff; `DeliveryJob.FailureSink` records the runtime's terminal failures. The delivery log
  (`foundry_webhook_deliveries`, generated `internal/webhookstore`) keeps state,
  attempts, last status and a failure category without payloads in output;
  `Queue.Replay`/`ReplayFailed` and the `webhooks` command (`deliveries`,
  `replay`, `prune`) inspect, replay and prune deliveries. `outbound.Module` owns the service.

- `outbound.Service.PruneDeliveries` and `webhooks prune --older-than`
  delete finished delivery-log rows (and their stored payloads) in bounded
  batches.

#### Performance

- S3 part buffers are sized by the declared size and pooled; multipart parts can
  upload concurrently (`PartConcurrency`).
- Attachment uploads hash the source in the reading pass (an image policy hashes
  only its output); full reads verified by the disk's stored checksum are not
  hashed again; `imaging.ProcessBytes` no longer copies its input.
- Brightness, contrast and grayscale operate on pixel rows with lookup tables
  instead of per-pixel `At`/`Convert`.
- Storage byte transfers reuse pooled copy buffers; framework-owned S3
  publication, part and inspection calls use `callback.Invoke` without an extra
  goroutine.

### Application lifecycle, realtime and operations

#### Fixed

- A WebSocket subscribe that met a different record of the same connection for
  the scope (left by a leave that failed transiently) was reported by Redis as
  a namespace `PolicyConflict`, stopping the hub and exiting the process. It is
  now `websocket.MembershipConflict`: the hub releases the stale record and joins
  again. Presence leaves, disconnects and restored entries advance the presence
  revision, and presence members are ordered by bytes in Go rather than by the
  Redis server's collation locale.
- Relaying to others (`RelayToOthers`, `ExceptConnection` of a local connection)
  no longer adds a `connection` member to the fan-out envelope, which earlier
  releases reject by stopping their hub, so a rolling upgrade no longer stops old
  instances. Excluding a connection on another instance requires the new
  `ClusterConfig.ExcludeRemoteConnections` once every instance is upgraded; an
  envelope this release cannot read is dropped alone.
- A startup deadline is reported as `fault.Timeout` and logged as `application
  startup failed` even when a provider returns only `ctx.Err()`, so CLI commands
  exit with a failure instead of the interrupt status 130.
- Maintenance allow networks matched the socket peer because admission runs
  before `TrustedProxy`; behind a load balancer, allowing its private subnet
  admitted every client. `http.WithAdmissionProxy` (passed automatically by
  configured applications with a global `TrustedProxy`) resolves the client and
  public scheme with the same policy, and the bypass cookie is `Secure` for a
  trusted https scheme.
- Maintenance bypass cookies were signed with the stored secret digest, so anyone
  able to read the shared store could mint one. They are now sealed with the
  application encryption keys (`maintenance.Policy.Keys`) and bound to the
  digest; without keys they are valid only on the issuing instance.
- An exhausted `MaxTotalQueuedBytes` disconnected whichever WebSocket connection
  was receiving the next frame, even one with an empty queue; it now disconnects
  the largest queue holders first and releases their bytes immediately.
- The public `/ready` route ran every dependency probe per unauthenticated
  request; it now reads lifecycle and maintenance state live and reuses one
  dependency check for `http.probes.readiness_cache` (default one second,
  `diagnostics.ReadinessCache`), refreshed by a single request, and no longer uses
  the diagnostics operation slots.
- `checks.Disk` treats a 403 on its probe key as answered, because S3 answers 403
  for a missing key without `s3:ListBucket`; the required permissions are
  documented.
- Adapter code calling `runtime.Goexit` in the cluster receive loop left the hub
  reporting a live stream nobody read; the stream is now recorded as lost and
  supervision continues on a new goroutine.
- A successful WebSocket authorization refresh extends the freshness window
  before waiting for in-flight messages, so that wait can no longer revoke the
  connection; the documented revocation bound now includes `PongTimeout` and the
  cluster lease renewal. Lease renewals are bounded by
  `min(ConnectionTTL/3, OperationTimeout)`, and their own deadline is a tolerated
  transient failure rather than a disconnect.
- Application shutdown now waits for the managed realtime publisher to exit
  before later cleanups close its Redis client (`websocket.Publisher.Done`).
- Queue depth sampling runs only in worker or scheduler processes (or without a
  selected kernel), not in HTTP replicas and CLI commands.
- `app.Migrations()` now includes the failed-job archive migrations when
  `worker.archive.enabled` is set, and the archive's default database and
  schema are selected and checked once before any resource is acquired, so
  registration and migration tooling agree on one target.
- Distributed WebSocket hubs no longer stop, and the process no longer exits, on a
  transient Redis failure. A failing per-connection operation (a latency spike,
  failover, evicted key or stepped Redis clock) fails only that upgrade, subscribe,
  message or trusted call with a retryable 503 or the new `unavailable` code and
  marks the hub degraded until an operation succeeds. Lease renewal failures are
  tolerated until the lease would expire, disconnect cleanup failures are counted
  and reconciled, and a Redis clock stepped back within the namespace retention
  window is clamped instead of rejecting every operation (a larger gap still
  fails each operation as corrupt state without rewriting it). Only a
  `websocket.PolicyConflict` or a failed initial subscription is terminal.
- A lost fan-out stream, including pub/sub buffer overflow, is now recovered: the
  hub resubscribes with jittered backoff (100 ms to 5 s), closes local sockets with
  status 1013 so clients reconnect and request replay (or keeps them with
  `ClusterConfig.RetainConnectionsOnGap`), and resynchronizes every active presence
  scope. Presence refreshes left the receive loop for a debounced, bounded worker;
  each envelope is decoded once and invalid envelopes are dropped and counted.
- The WebSocket inbound queue default rose from 4 to 64 frames and generated
  TypeScript clients now keep in-flight operations within
  `min(subscriptions, inbound_queue)` and within the exported message rate.
  `malformed` replies carry the request ID when it was decoded, and
  `rate_limited` replies (checked before decoding) when it is among the frame's
  first scalar members, as generated clients send it; clients treat uncorrelated
  error replies as non-fatal.
- WebSocket per-connection rate limiting uses a monotonic token bucket, so NTP
  wall-clock steps no longer disconnect clients.
- A graceful signal stop of a service kernel now returns nil from `App.Run` (exit
  status 0 through `cli.Report`) instead of `context.Canceled` joined with a
  shutdown timeout; executables no longer call `Shutdown` again. An interrupted CLI
  command still reports its cancellation.
- `App.Run` reports a kernel or boot failure once instead of joining it with the
  same failure from shutdown.
- Lifecycle cancellation classification reuses the shared bounded error walker.
- Maintenance admission no longer depends on observability. Every application owns
  one gate (`services.Maintenance()`, `App.Maintenance()`), carried by the runtime
  context through `maintenance.FromContext` and shared with a configured recorder;
  `features.observability.maintenance` now works with observability disabled. HTTP,
  CLI, scheduler and diagnostics readiness consult that gate, falling back to a
  context recorder's gate for manual compositions.
- Readiness probes within one check now run concurrently, each under its own
  timeout, so one slow dependency no longer consumes the whole readiness budget.
  Results keep declaration order and the shared concurrency bound is unchanged.
- File log sinks no longer lose records when retention cleanup or rollover fails.
  A failed cleanup is counted and retried after one minute, doubling up to an
  hour, instead of rescanning and failing every write; a failed rollover keeps
  appending to the current file with the same backoff. Startup cleanup failures
  no longer prevent startup. Only a missing active file that cannot be reopened
  fails a write.
- Destination write failures are counted per sink. File, stdout and syslog sinks
  rewrite a failed record of at most 256 KiB to stderr; the first failure of each
  kind emits one fixed-text stderr notice with a redacted diagnostic and errno,
  never error messages or payloads. Stacks keep delivering to healthy children.
- `observability.Observe` now derives the outcome only from the callback's
  returned error: work that returned `nil` stays `Succeeded` even when its
  context is cancelled or expires afterwards. `tracing.Start` and
  `Recorder.Start` no longer reject an already-cancelled context, so cancelled
  operations keep their span, metrics and `Cancelled` outcome instead of being
  counted as dropped spans.

#### Changed

- `application.AboutCommand` reports a shared or publisher-only realtime
  configuration.
- `Settings.ShutdownTimeout`/`foundation.WithShutdownTimeout` is now one budget
  that starts with shutdown and covers the stop delay, kernel drain and all
  cleanups, which share its remaining deadline; `foundation.DefaultShutdownTimeout`
  is now 25 seconds. Configured assembly rejects budgets where the stop delay plus
  an enabled listener's shutdown grace (plus the WebSocket drain for a dedicated
  realtime listener) leaves no time for cleanup.
- WebSocket queue memory is bounded by actual queued bytes (`MaxQueuedBytes`,
  default 1 MiB per connection; `MaxTotalQueuedBytes`, default 256 MiB per hub)
  instead of a worst-case product, so defaults rose to 10,000 connections per
  process and 256 outbound frames; cluster `MaxConnections` accepts up to 1,048,576
  and defaults to 65,536, and the fan-out buffer defaults to 4,096 messages / 32 MiB.
  Changing cluster `MaxConnections` changes the namespace policy fingerprint: pin
  the previous value (1,024) during a rolling upgrade or use a new namespace.
- WebSocket trusted publication, presence and disconnect operations use a separate
  `Config.MaxOperations` (default 1,024) with a bounded wait and report
  `fault.Overloaded` instead of failing immediately at socket capacity.
- Incoming WebSocket messages reuse the connection's authorization freshness
  window (current auth scope and each subscription's last authorization) instead of
  resolving credentials and re-running channel authorization per frame. Subscribe
  still authorizes freshly; the periodic refresh, now jittered by ±10%, replaces
  the cached state or disconnects.
- The maintenance gate exists even when observability is disabled. The runtime
  context carries it for `maintenance.FromContext`; websocket upgrades and
  operations use it.
- `diagnostics.Runtime` readiness and status report the application gate's mode.
  Schemas whose environment names differ only by a `_FILE` suffix are rejected.
- Retention cleanup after rollover and hourly writes runs in one worker owned by
  the sink, so record writes never scan the log directory. Close stops the
  worker before releasing the file and rotation lock.
- Trace sampling adds `observability.Config.TraceSampleRatio` (application
  `features.observability.trace_sample_ratio`), sampling that fraction of traces
  deterministically from the trace ID so every span and process agrees. Zero
  selects every trace; `SampleTraces = false` still disables sampling.
  `tracing.StartSampled`, `tracing.Sampler` and `tracing.RatioSampler` expose the
  same policy.

#### Added

- Opt-in housekeeping schedule: `features.maintenance.enabled` registers
  leader-only, non-overlapping `foundry.maintenance.<task>` schedules in
  processes running the scheduler kernel for every enabled bounded store with a
  prune API: published outbox rows past retention (30 days), expired
  idempotency outcomes (only while the store's own pruner is disabled, never
  both), audit entries past `retention_days`, archived failed jobs (30 days) and
  read notification inbox records (90 days). Applications add guards, prunable
  models and other stores through `FeatureDeclarations.Pruning` with
  `application.PruneSessions`, `PruneTokens`, `PruneModels` (selections derived
  again at each run from the application clock) and `PruneWith`. Each task
  removes bounded batches (`batch`, `max_batches`, `timeout`, `interval`,
  `retention`, `disabled` per task or under `defaults`), logs committed
  removals and leaves failures to the scheduler's logging and observation;
  Build rejects out-of-bound settings before any resource is acquired.
- Configured HTTP kernels install `database.StickyReadsHandler` as the
  outermost global middleware (`application.StickyReadsMiddlewareID`) when a
  database connection enables its read pool and sets `sticky_read_window`, so
  each request reads its own writes; nothing is added otherwise.
- `foundation.WithStopDelay`/`Settings.StopDelay`: a lame-duck period in which
  readiness fails while listeners keep serving before admission closes.
- `foundation.WithStartupTimeout`/`Settings.StartupTimeout`, and structured
  lifecycle log events for startup, readiness, shutdown, cleanup failures/timeouts
  and completion with redacted diagnostics. The completion record is written
  before `Run`/`Shutdown` return, so an exiting process keeps it.
- `App.RunKernels` runs several service kernels (for example HTTP and a worker) in
  one process; `foundation.WithMaintenance`, `App.Maintenance` and
  `Runtime.Maintenance` expose the application gate.
- `Realtime.Shared` serves WebSocket upgrades from the application HTTP listener
  (`websocket.SharedModule`, `websocket.Route`); `Realtime.Publisher` and
  `Services.RealtimePublisher()` provide a managed publisher for HTTP handlers and
  jobs in processes without a hub.
- `websocket.ExceptConnection` and `Incoming.RelayToOthers` skip live delivery to
  the sender; `Hub.Probe` is an I/O-free readiness check; `websocket.WithLogger`
  logs cluster degradation, stream loss/recovery and terminal faults.
- Hub snapshots report streaming state, gaps, resubscriptions, dropped envelopes,
  operation overloads and queued bytes; diagnostics report cleanup failures.
- Fleet-wide maintenance: `maintenance.State` (down flag, Retry-After, public
  message, bypass secret digest, allowed networks, `[METHOD ]/path[/*]` exemption
  rules), `maintenance.Store`, `Refresh`, `Publish` and `Watch`, and
  `maintenance/cachestore` over any configured cache store (Redis or PostgreSQL for
  several hosts). `maintenance.store`/`poll_interval` apply the shared record at boot
  and poll it; a store outage keeps the last applied state and logs once.
  `application.MaintenanceCommands()` provides `down` (`--retry`, `--message`,
  `--secret`/`--with-secret`, `--allow`, `--except`) and `up`. Paused HTTP responses
  keep the standard error envelope with the operator message and `Retry-After`, and
  are not logged as server failures. Visiting `/<secret>` issues a signed HTTP-only
  bypass cookie valid on every instance until the secret changes; only the secret's
  SHA-256 digest is stored. `maintenance.exempt`/`allow` add configured exemptions
  beyond exact GET/HEAD `MaintenanceReadPaths`. Draining still rejects everything.
- CLI commands declared with `Command.AllowDuringMaintenance()`, or invoked with the
  leading `--during-maintenance` flag, run while maintenance is paused (never while
  draining). `cli.FlagsWithArgs`/`cli.ExactArgs` bind typed positional arguments;
  `cli.Confirm`/`cli.ConfirmInProduction` require an interactive `y`/`yes` unless
  forced; `cli.WriteTable` writes aligned, control-character-safe tables;
  `application.AboutCommand` prints the framework/Go versions, platform, namespace,
  kernels and configured drivers without hosts or credentials.
- Public probes: `http.probes.liveness`/`readiness` mount unauthenticated `/up` and
  `/ready` routes through `diagnostics.PublicProbe`, returning only a status body
  (liveness performs no dependency checks; readiness reuses diagnostics readiness).
  Probe paths stay reachable while paused. Authenticated diagnostics are unchanged.
- Opt-in profiling: `diagnostics.Profile` serves bounded `runtime/pprof` heap,
  goroutine, allocs, block, mutex, threadcreate, CPU and trace captures through an
  authenticated diagnostics route when `diagnostics.Config.Profiling` is set, one
  capture at a time, without registering on `http.DefaultServeMux`.
- `health/checks` adds read-only disk (`checks.Disk`) and mailer admission
  (`checks.Mailer`) probes; configured assembly adds the realtime hub probe with
  `configured_connections` and disk/mailer probes with
  `features.health.configured_storage`/`configured_mail`.
- Configuration: `NAME_FILE` environment variables read a value from a bounded
  regular file (one trailing newline removed; setting both is an error), and with
  `Inputs.Environ` (e.g. `os.Environ`) per-entry variables such as
  `APP__SERVICES__DATABASE__CONNECTIONS__MAIN__PRIMARY__PASSWORD` override one field
  of one named connection, merging into the supplied collection and decoding
  through its generated element schema.
- `Sink.Stats()` and `ChannelSet.Stats()` report typed per-channel delivery
  counters (records, failures, stderr fallbacks, dropped, queued, rotation and
  retention failures) for metrics.
- Optional asynchronous sinks (`sink.async.enabled`, bounded `queue` and
  `max_bytes`) write through one owned writer, drop and count the newest record
  with `fault.Overloaded` when full instead of blocking, and drain on Close.
- A `syslog` sink driver (macOS/Linux) with udp/tcp/unix/local daemon, tag and
  facility settings maps record levels to syslog severities.
- Custom `slog.Handler` channels through `application.WithLogHandler` and
  `logging.WithHandler`, with a deployment-selected minimum level via the
  `custom` driver and strict binding validation.
- `logging.WithAttrs` attaches bounded, redacted request-scoped fields that JSON
  loggers emit under a reserved `context` group; `logging.ContextFields` snapshots
  them as bounded JSON for job payloads and restores them in handlers.
- Prometheus exposition (`Recorder.WritePrometheus`, served by the authenticated
  diagnostics metrics endpoint) now includes Go runtime metrics read without
  stopping the world, process start/uptime metrics, CPU seconds and descriptor
  limits on Unix, and open descriptors/resident memory on Linux.
- Typed metric collectors: `Recorder.RegisterCollector(name, collector)` with
  validated/escaped `MetricWriter.Gauge`/`Counter` samples and `Label` values,
  per-scrape callback isolation, bounded collectors/samples/labels and a
  `foundry_collector_failures_total` counter. Configured applications register
  database pool, Redis pool, log channel (`foundry_log_*{channel}` delivery,
  fallback, drop, rotation and retention counters) and realtime hub collectors
  automatically when observability is enabled.
- `TraceBatchExporter`/`Config.TraceBatchExporters`/`TraceBatchSize` export queued
  spans in bounded batches without waiting to fill them, and
  `observability.NewOTLPExporter` posts them as OTLP/HTTP JSON using only the
  standard library: explicit redacted headers, no redirects, one attempt per
  batch, no payloads or vendor state. Applications add it with
  `application.WithTraceBatchExporter`.
- `observability.LogReporter` logs each failed operation once as a structured
  record with the redacted diagnostic and correlation IDs, suppressing duplicate
  fingerprints within a window (reporting the suppressed count afterwards), with
  bounded fingerprint retention and `DontReportFaults`/`DontReportStatuses`/
  `DontReportOutcomes` filters. Configured applications enable it with
  `features.observability.error_log.enabled`.

#### Performance

- WebSocket publication routing, presence notification, subject quotas and
  subject disconnects use subscription and subject indexes instead of scanning
  every connection; counters are atomic and queued-byte accounting no longer takes
  the hub lock.
- Each WebSocket connection uses one maintenance timer (pings, authorization
  refresh, lease renewal) and a plain token bucket instead of separate tickers and
  a rate-limit store; request, publication and cluster envelopes are decoded in one
  strict pass; framework publication steps and cluster receive no longer spawn an
  isolation goroutine per message.
- Redis WebSocket lease renewal touches only the connection's own entries instead
  of loading every presence member, shared metadata is rewritten only when it
  changes or ages (an unchanged presence read no longer advances the revision),
  and history appends are skipped for channels without replay. Inbound frames
  are rate-limited before they are decoded.
- The observation recorder no longer serializes every span on one mutex: span
  admission uses an atomic counter, metric series live in hash-selected shards
  with atomic counters, the recent ring locks individual slots, and snapshots
  sort after releasing their locks. Trace/span IDs come from pooled ChaCha8
  streams seeded from the operating system's cryptographic source rather than a
  system call per ID. On an Apple M4 Max the parallel span benchmark measured
  about 430/195/295/505 ns per span at 1/4/8/16 CPUs versus 605/755/1040/1190 ns
  before.

### Validation, localization, reporting, settings and audit

#### Fixed

- Model-content translations and UI catalogs now share one parent-locale rule,
  `i18n.LocaleSet.Match`: `Resolve` tries the requested locale, its supported
  regional parents (`en-GB` → `en`, `zh-Hant-TW` → `zh-Hant`), then the
  default, then the remaining supported locales. As in CLDR, a script subtag
  directly after the language ends the chain, so `zh-Hant` never falls back to
  `zh` nor `sr-Latn` to `sr`, which may use another writing system. UI lookup
  tries the requested locale, its supported parents, then
  `CatalogOptions.Fallback`, and still never an unrelated sibling.
  `LocaleSet.Fallbacks` (also used by localized attachments) gains the same
  parent step.
- `translations` `Load` pages through translation rows with keyset pages of
  4,096 rows in the same snapshot, so batches of owners × locales beyond one page
  load completely instead of failing. Rows remain bounded by owners × supported
  locales and text by `MaxBatchBytes`.
- `i18n.Load` ignores dot entries (`.DS_Store`, `.git`, `.gitkeep`) at every level
  and regular files without a `.json` suffix. Root `.json` files, nested
  directories and symlinks still fail. Every catalog, declaration and argument
  error names its locale, file, key and parameter where known, with a fixed
  reason and bounded ASCII quoting; template text, JSON values and argument
  values are never included. A duplicate key names the file that first defined it.

- Large audited values no longer fail or roll back the business write. A value
  above the configurable `audit.Config.MaxValueBytes` (default 64 KiB) is stored
  as a `record.Oversized` marker with its byte size and SHA-256 digest; JSON is
  digested only after sensitive keys are redacted. When a whole record would
  still exceed its representation bound, the largest values are digested until
  it fits. Reading a digested value returns `fault.Missing`, never a fabricated
  value.
- Audit history written in one transaction keeps its actual order. Rows gain a
  database-assigned insertion `sequence`; history is ordered by it instead of the
  shared transaction timestamp and a random ID tiebreak. The new
  `000002_add_history_keys` migration backfills existing rows in their previous
  `(created_at, id)` order.
- Extending sensitive-name redaction no longer invalidates stored history. Every
  row records the redaction policy that wrote it (`record.Redaction`), and reads
  validate model snapshots and domain documents under that policy. Values that
  the current policy treats as sensitive are then masked on read, so an older
  row no longer returns an `otp`, `card_number` or `session` value, or such JSON
  keys: sensitive columns read as redacted, disclosed JSON with a newly
  sensitive key is redacted completely, and already redacted JSON also redacts
  the new keys. Masked model entries stay valid under their capturing policy;
  a masked domain document is redacted and reports `CurrentRedaction`.

- Datatable `like` filters are disabled until a declaration calls
  `FilterSource.AllowLike()`, matching the documented contract; client patterns
  can no longer reach text columns that only declared literal search.
- Negated datatable filters mean "not true": `ne` and `not_in` on nullable
  columns and `not` groups (pushed down to each comparison) now include rows
  whose operand is NULL instead of silently dropping them. NULL tests negate
  exactly and double negation restores the original filter.
- One awkward value no longer fails a datatable export: invalid UTF-8 becomes
  U+FFFD, CSV carries control characters literally, XLSX escapes C0 controls
  and U+FFFE/U+FFFF as `_xHHHH_`, and cells above `MaxCellBytes` or the 32,767
  UTF-16 unit spreadsheet limit are truncated with an explicit `…[truncated]`
  marker. `MaxCellBytes` must now be at least 64.
- Datatable downloads send the artifact SHA-256 as a strong `ETag` plus
  `Last-Modified`, so a Range resume with `If-Range` receives the complete new
  file when a regenerated report differs instead of splicing two runs.
- Datatable downloads no longer end as a 503 while generation keeps an export
  slot, a read transaction and a temporary file until `ExportTimeout`.
  Generation and transfer run under the request's single deadline: declare the
  download route with `.WithTimeout(manager.DownloadTimeout())`
  (`Config.DownloadTimeout()`, twice `ExportTimeout`, 20 minutes by default).
  When the request ends through its deadline, a client disconnect or shutdown,
  generation stops and promptly releases its slot, transaction and file.
  Completed artifact reads no longer end with the generation lease.
- `datatable.Define` rejects searchable columns that span the WHERE and HAVING
  phases instead of failing every search request at runtime.

- Model extension owners can declare a stable persisted identity with
  `extensions.DefineOwnerWith(name, source, extensions.OwnerOptions{StorageModel: "users"})`.
  The owner scope, row keys and attachment object paths then depend on the
  declared owner identity instead of the current table name, so renaming an owner
  table no longer hides its metadata, translations and attachments; identities
  stored under the old table decode through the current key codec. The default
  remains the table name, which keeps every existing scope unchanged. Rows already
  hidden by an undeclared rename are recovered by declaring the old table in
  `OwnerOptions.PreviousModels` and running `metadata rescope` /
  `translations rescope` (`InspectStale`/`Rescope`), which verify each row, adopt
  only rows whose recorded scope and identity name a declared model, skip rows
  whose subject no longer exists, move rows in bounded locked pages only with
  `--apply`, and never overwrite an existing current-scope row; skipped rows are
  reported as `undeclared`, `missing` or `conflict`. Natural-key cleanup after bulk deletes is documented.
- An incompatible stored setting version no longer boots successfully and then
  fails every `Get`. Keys declare conversions with `settings.UpgradeFrom` in
  `RegistrationWith(settings.Options[V]{Upgrades: ...})`; reads convert older rows
  in memory, and `settings.Reconcile` persists upgrades at startup or fails with
  `fault.Conflict`, naming each incompatible setting without writing anything.
- Setting presentation no longer drifts from declarations. A presentation written
  from a declaration is refreshed by `Ensure` and `Reconcile`; `Configure` takes
  explicit ownership (`Record.Configured`) and `ResetPresentation` hands it back.
  The `000002_add_presentation_ownership` migration treats existing rows as
  configured, so no stored presentation changes without an explicit reset.
- Settings listings no longer fail above 4 MiB. `settings.ListPage` returns keyset
  pages that end early at the byte budget; `List` reads consecutive pages up to
  `MaxLoadBytes` (64 MiB); `Groups` reads presentation columns only.
- At most one default country can exist: the `000002_single_default_country`
  migration adds a partial unique index and refuses to guess when existing data
  has several defaults. `countries.Default` and `countries.SetDefault` read and
  atomically move the default.

#### Changed

- Updates, soft deletions and restorations now audit only fields the write
  assigned or changed, plus the subject key; unchanged columns are neither
  encoded nor stored. Creation and deletion keep complete snapshots. Generated
  models call the new `record.Capture`; previously generated adapters still
  compile and receive the same filtering.
- Sensitive-name redaction (policy 2) splits letter/digit boundaries, ignores
  digit suffixes and simple plurals, recognizes joined names such as
  `accesstoken`, and adds passphrase, OTP, PIN, CVV/CVC, SSN, card number,
  cookie, session and recovery-code conventions. Short abbreviations match whole
  words only.

- Datatable export capacity has two levels: `MaxExports` bounds concurrent
  generation and is released when the artifact is complete; the new
  `MaxArtifacts` (default 16, at least `MaxExports`) bounds completed artifacts
  still open for delivery. Exhausted capacity waits briefly and then reports
  `fault.Overloaded` (HTTP 503) instead of `fault.Conflict`.
- `Manager.Close` closes completed artifacts that are still open (their reads
  report `fault.Closed`, their files are removed) instead of waiting for their
  owners, so module shutdown waits only for callbacks that use borrowed
  providers. Export-job delivery contexts end at manager shutdown and still
  reject closing their own manager as a cycle.
- The default datatable `MaxOffset` is 10,000 rows instead of 1,000,000.
- XLSX exports write integers and decimals with at most 15 significant digits,
  floats, booleans, dates and date-times as typed spreadsheet cells with ISO
  number formats; date-times use the export's presentation time zone. Longer
  integers (such as 16-digit identifiers) and decimals, enumerations and
  `FormatWith` output remain exact text, so a spreadsheet never rounds them.

- The model extension store admits 64 concurrent operations by default (was 32).
  Settings, metadata and translation bursts during database latency queue for a
  bounded wait before `fault.Overloaded` instead of failing.

- `ExistsAll` now reports each missing value at its index path with the
  collection's label, instead of one issue at the collection path. Valid lists
  still cost one batched observation; rejected lists are bisected with bounded
  extra observations. The `validation.exists_all` English text is now
  "{{attribute}} contains an invalid selection."
- Rule trees without application callbacks run inline instead of in a goroutine
  per `Check`. Panics in selectors and value methods remain internal failures;
  `runtime.Goexit` in those inline calls now ends the caller like any Go call.
  Trees containing `Custom`, `Dynamic`, `Hook`, lookups, `Provide` or image
  measurement still run in one owned goroutine that contains Goexit.

#### Added

- `decimal.Decimal` adds explicit rounding: `Round(scale, mode)` and
  `Div(divisor, scale, mode)` with `HalfUp`, `HalfEven`, `Down`, `Up`, `Floor` and
  `Ceiling` modes (the zero mode is invalid), plus `Sign`, `Neg`, `Abs`,
  `decimal.Min`/`Max`/`Sum`, exact `decimal.Scaled(coefficient, scale)` and
  `Fixed(scale)` presentation, which fails instead of rounding. Division by zero
  and results beyond `MaxDigits` fail.
- The new `decimal/money` package pairs exact amounts with ISO 4217
  `money.Currency` codes and minor units. Constructors never round implicitly;
  arithmetic requires one currency; `Multiply` names its rounding mode;
  `Allocate`/`Split` never create or lose a minor unit. JSON is a strict
  `{"amount":"12.30","currency":"USD"}` object.
- The new `i18n/numberformat` package formats exact decimals, fixed-scale values,
  percentages and money for a locale with CLDR digits, separators, grouping
  (including Indian grouping), signs, percent patterns and currency symbols from
  `golang.org/x/text`, without floating-point conversion. Currency placement
  follows a documented per-language table and can be overridden.
- The new `str` package adds rune-safe `Slug`, `Limit`, `Truncate` and `Mask`,
  English `Plural`/`Singular`/`Pluralize` and IEC `HumanFileSize`.
- `collection` adds `Chunk` (owned, capacity-capped chunks), `Unique`, stable
  key-based `SortBy`/`SortByDesc`, `MinBy`/`MaxBy`, `CountBy`, `Sum`/`SumBy` and
  float64 `Average`/`AverageBy`.

- `i18n.LocaleResolver[S]` selects a locale for a typed subject (HTTP request,
  user, notification recipient) from ordered steps — `ContextLocale`, a named
  stored `Preferred` lookup and `AcceptLanguage` — then the default, and reports
  the selecting `PreferenceSource`. Unsupported stored locales fall back to a
  supported parent or defer; a malformed stored value such as `en_US` is no
  preference and is reported in `Resolution.Ignored` instead of failing every
  request; lookup failures, contained panics and cancellation return errors
  instead of silently choosing the default. `WithResolvedLocale`
  records the selection on the context. `LocaleSet.MatchTag` and
  `LocaleSet.MatchAcceptLanguage` expose the shared matching rules.
- The `000002_index_translation_values` translation migration adds a hash index on
  translation values for `Matching` exact lookups, including 64 KiB values.

- Keyset audit reads: `audit.ModelTimeline`, `Action.Timeline` and
  `audit.ActionTimelineFor`, plus payload-free `audit.Activity` timelines by
  subject, model actor, system identity and correlation
  (`SubjectActivity`, `ActorActivity`, `SystemActivity`, `CorrelationActivity`),
  backed by indexed, database-generated lookup keys. Records expose `Sequence`,
  `Correlation` and `Route`.
- `audit.WithCorrelation`/`audit.NewCorrelation` group the rows of one logical
  operation; rows otherwise correlate by attribution request ID.
- `attribution.Route` carries the matched request method and route name for the
  current request; audit rows store it. Raw URLs and path values are never stored.
- `Scope.PruneRetention`/`Scope.PruneBefore` prune in bounded per-transaction
  batches, and `application.AuditCommand()` declares the explicit
  `audit prune` operator command. It only counts matching entries unless
  `--apply` is given and rejects a `--before` cutoff later than now;
  `Scope.CountBefore` and `Scope.RetentionCutoff` expose the same count and
  cutoff.

- `ExportOptions.ByteOrderMark` prefixes CSV exports with a UTF-8 byte-order
  mark for spreadsheet programs that guess legacy encodings.
- `Table.SimpleQuery` returns a `query.SimplePage` from one lookahead row query
  without counting, for sources where a total is not needed.
- The `datatable/importer` package streams CSV and XLSX uploads into typed row
  structs: declared heading-to-field columns with scalar codecs, per-row
  validation rules, row-numbered failure reports, chunked handler calls and
  bounds on rows, columns, cells, records, input, decompressed XML (128 MiB by
  default), every XML token (text, attribute, comment, CDATA and processing
  instruction, whatever `<` or `>` they contain), shared strings and ZIP
  directory size; document type declarations are rejected. XLSX support covers
  shared, rich and inline strings, formula results, booleans and serial dates
  in a declared zone. A stored number that 15 significant digits cannot
  represent, such as a 16-digit identifier, is a `type` issue in every
  non-float column instead of a silently rounded value, float columns receive
  the stored double exactly, and rows of only error cells are reported as
  failures rather than skipped as blank.

- Settings keys can opt into a per-process, manager-owned read cache with
  `RegistrationWith(settings.Options[V]{Cache: ttl})` (at most one hour). Hot
  `Get`/`GetOr`/`Find`/`Load` calls perform no query; writes through the manager
  invalidate before, after and, for joined transactions, after commit. Other
  processes observe a change within the TTL; `Manager.Invalidate` drops entries.
- `settings.Load(ctx, manager, keys...)` and `settings.LoadGroup(ctx, manager, group)`
  read several typed settings with at most one query; `Key.From`/`FromOr` decode
  them from the batch without I/O.
- `model.Identity.WithModelName`, `extensions.Registry.AdoptSubject`,
  `Registry.DeclaresModel`/`DeclaresScope`, `Owner.RecordedModels`,
  `extensions.OwnerOptions.PreviousModels` and `extensions.Store.Clock` support
  the owner identity and cache features. `datatable.Config.DownloadTimeout` and
  `Manager.DownloadTimeout` give the download route deadline.

- Validation rules can be parameterized at check time. A typed `validation.Slot`
  is filled per check by `validation.Provide` from the context and the input at
  its level, such as an HTTP path key; `slot.Value(ctx)` fails rather than
  returning a zero value outside its provider, and `validation.Requires` makes
  endpoint registration reject a slot reader without one.
  `databasevalidation.UniqueIgnoring` excludes the row being updated, and
  `databasevalidation.Scoped` derives a lookup's model scope (for example a
  tenant filter) at every check, so handlers no longer rebuild rules per request.
- `ExistsEach`/`UniqueEach` validate one selected field of every object in a list
  with batched observations and report at element paths such as
  `/items/3/product_id`; `UniqueAll` checks scalar lists. `databasevalidation`
  provides typed adapters, `databasevalidation.Lookup` exposes the typed lookup,
  and `query.ValueLookup.AnyExist` adds batched `IN` observations.
- Relative temporal rules `AfterNow`, `AfterOrEqualNow`, `BeforeNow`,
  `BeforeOrEqualNow`, `AfterToday`, `AfterOrEqualToday`, `BeforeToday` and
  `BeforeOrEqualToday` read an injected `temporal.Service` clock at every check,
  compare "today" in its timezone and have their own message keys. A zero
  `temporal.Service` is an invalid declaration, never a silent host clock in UTC.
- Decimal field comparisons (`DecimalGreaterThan` and siblings),
  `DecimalMaxPlaces`, exact `DecimalMultipleOf`, map `EachKey`/`EachValue` with
  deterministic entry paths, Go-layout `DateFormat`, `DistinctIgnoringCase`,
  image `Dimensions` (with `validation/imaging` inspecting uploads through
  `imaging.Inspect`), `Hook` reports that add issues at arbitrary relative paths,
  and `Dynamic` rules whose literal or typed translated message is chosen at
  check time.

#### Performance

- `decimal.Decimal.Cmp` compares canonical digits directly and no longer
  allocates `big.Int` values.
- Enum descriptors validate once per `Describe` result, so retained descriptors
  no longer repeat JSON checks on every `LabelKey`, `Label`, `LabelDefinitions` or
  `Definition` call. A validation interrupted by a panicking custom marshaler stays
  invalid instead of being cached as success.
- `sanitize.StripTags` reuses one immutable empty-allowlist policy instead of
  building a bluemonday policy per call.

- `WithLocale` and `SnapshotLocales` read the framework's `LocaleSet` and
  `*Catalog` directly instead of starting an isolation goroutine per call
  (about 530 ns/9 allocations to 50 ns/2 allocations per request); application
  `LocaleCatalog` implementations remain isolated.
- Catalogs parse plural language tags and lookup chains once at construction.
  `PreparedMessage.Format` checks the catalog signature without copying and
  renders its already validated arguments; `WithText` shares the immutable
  definition and fallback. The validation-message path (label substitution and
  rendering) fell from about 1.6 µs/13 allocations to 0.3 µs/4 allocations, and
  `Catalog.FormatDynamic` from 5 allocations to 1.
- Typed `message.Message` resolves its signature and per-parameter wire metadata
  once at definition instead of on every `Format`.
- Translation `Set` writes all assignments with one set-based
  `INSERT ... ON CONFLICT`; `Clear`, `DeleteAll` and `Cleanup` use one set-based
  `DELETE` instead of one statement per row. Existing-row checks and `Matching`
  select ownership columns only, never translation text.

- Audit entries are validated and decoded once when built or read instead of on
  every `Validate`, `Payload`, `RestoreModel` and `Inspect` call, and stored
  snapshots no longer re-parse each value as a separate JSON document.
- Subject history uses a bounded btree lookup key with keyset paging instead of
  a hash index on the whole subject document.

- Datatable counts omit the table's `ORDER BY`.
- Exports no longer JSON-encode every row just to measure it; the formatted
  cells are bounded by `MaxRowBytes` instead. Output is buffered before the
  file, and XLSX rows are encoded into one reused buffer and buffered before
  the compressor instead of many small formatted writes.

- Valid input no longer allocates JSON Pointer paths or issue storage, and
  `Parallel` branches no longer start a second goroutine each. A valid nested
  field/optional/collection check allocates only its execution state.

### Generation, contracts, TypeScript and testing

#### Fixed

- Recursive generation skips cgo packages and platform-only packages that contain
  no Foundry declarations instead of failing the whole run. A cgo package with
  declarations still fails, naming its source file; declarations in a file
  excluded by the host's build constraints fail with an actionable error instead
  of having their owned output deleted as obsolete.
- Generated file headers name the declaration's source file without a line
  number (`// Source: models.go.`), so adding a line above a declaration no
  longer rewrites every generated file below it or fails `generate --check`.
- Enum discovery counts only exported constants declared as values of the enum
  type. `const DefaultStatus = StatusActive` (an alias of a case) and constants
  marked `//foundry:ignore` (on the constant or its const group) no longer
  become duplicate wire values; an unexported value such as a `statusCount`
  sentinel must be marked `//foundry:ignore` or exported (see below).
- Generator diagnostics use module-relative paths (`models/user.go:12:6`) instead
  of bare basenames. `generate --check` lists each stale file with its reason:
  content, comments, formatting, managed field notes or ownership manifest.
- Generated helpers of models, projections, paths, queries, multipart forms and
  DTOs are checked against handwritten declarations and against each other; a
  collision reports both source positions instead of a later "redeclared" error.
- Generation honors vendor mode when the module keeps `vendor/modules.txt`
  instead of forcing `-mod=readonly`.
- `testkit/http` requests are bounded by `DefaultTimeout` and accept
  `WithTimeout` and `WithHeaders`.

- Generation no longer skips declaration checking for files that use the spaced
  directive form (`// foundry:model`), which directive parsing accepts. Such a
  package previously failed with a misleading "Foundry declarations require an
  exported defined type"; build-constraint and cgo detection use the same check.
- An unexported typed constant with its own value is no longer silently dropped
  from an enum's cases after regeneration. Generation fails at its position and
  asks to export it or mark it `//foundry:ignore`; `docs/compatibility.md`
  records the change.
- `testkit/http.WithHeaders` replaces an inherited header instead of adding a
  second value, and `ActingAsSession` replaces only the session pair in a single
  `Cookie` header. Re-authenticating an already-authenticated test client no
  longer sends duplicate credentials that the framework rejects with 401.
- `testkit/factory` runs `AfterCreating` hooks in the same transaction as the
  insert (a savepoint inside the caller's transaction). A failing or panicking
  hook now rolls back its model and, for `CreateMany`, the whole batch, even
  with a pool writer, as the `CreateMany` documentation states.

#### Changed

- `foundry generate` no longer rewrites handwritten model files. Managed "Foundry
  field behavior (generated)" notes are opt-in with `foundry generate
  --field-docs`; existing notes are left intact without it. The repository's own
  `make generate` opts in.
- `foundry generate` and `foundry doctor` compare the tool's framework module
  version (from build information) with the version the consumer's `go.mod`
  selects. A verified mismatch fails generation and the doctor `tool-version`
  check; development builds and local replacements are reported as unverifiable.
  `doctor` also prints the tool's framework module version next to the plugin API
  version. `plugin.FrameworkVersion` remains the declared plugin API contract.
- TypeScript clients decode server output tolerantly by default: responses, error
  envelopes, server events and presence ignore unknown object properties, surface
  unknown enum values as `UnknownEnumValue` (and omit map entries keyed by them)
  instead of failing. `ClientOptions.strictResponses` and
  `RealtimeOptions.strictEvents` restore strict decoding; requests and published
  events stay strict. Received types (`XReceived`, `ReceivedContractTypes`,
  `decodeReceived`) admit `UnknownEnumValue`, and TypeScript requires narrowing
  before such a value is sent back.
- Generated TypeScript and OpenAPI schema names are readable: the Go type's own
  name with generic arguments (`Page_Invoice`, `List_Invoice`), qualified with
  trailing package path elements only on a collision or when it would shadow a
  declaration of the generated module (`Billing_Invoice`, `Api_Map`), with a
  digest suffix only as a last resort. Names no longer change when a package
  moves; select types through `ContractTypes[...]` by identity to be immune to
  renames caused by a new same-named type.
- The TypeScript module parses its embedded manifest lazily on first use rather
  than at import, and the per-object known-property set is built once per schema
  instead of per decoded object. Client artifacts may be up to 48 MiB, enough for
  a module embedding the largest accepted 16 MiB manifest.
- Generated DTO JSON/message descriptors, validation field sets, path, query and
  multipart descriptors, enum descriptors and configuration keys are built once
  per process (`sync.OnceValue`) instead of recompiling schemas on every call.

#### Added

- `//foundry:dto role=response` declares a response-only DTO; its codec and schema
  are unchanged and no request `ValidationFields` are generated.
- `value.List[T]`, an ordinary slice type whose JSON is never null: nil encodes as
  `[]`, decoding rejects `null`, and generated schemas, OpenAPI and TypeScript
  describe a non-nullable array.
- Route and endpoint documentation: `WithDocumentation(http.RouteDocumentation{
  Summary, Description, Tags, Deprecated})` is exported in the manifest, as
  OpenAPI `summary`/`description`/`tags`/`deprecated` (with a top-level tag list)
  and as TSDoc on the TypeScript `API`. `WithBodyExample` and
  `WithResponseExample` take typed values, encode them through the endpoint's own
  contracts at declaration and publish them as OpenAPI examples.
  `openapi.Options.Servers` adds validated server entries.
- `typescript.ExportCommand` declares an application CLI command that builds the
  client contract from the application's own registries and publishes the SDK,
  manifest and OpenAPI (`--dir`, `--prefix`, `--check`), replacing a hand-written
  `manifest.Build` export program.
- `foundry make` scaffolds `endpoint` (request/response DTOs, their generated
  codecs, typed route, endpoint and handler stub), `enum --cases`, `event`,
  `listener --event`, `policy --subject --resource` (denies until implemented),
  `middleware`, `rule` and `notification` (payload DTO, codec and definition), and
  `make migration --create <table>` starts from a reviewed `CREATE TABLE`.
- `testkit/http` assertions: `AssertJSONPath` (JSON Pointer, typed expected
  value), `AssertValidationErrors`, `AssertHeader`, `AssertCookie`,
  `AssertRedirect`, `AssertNoContent`; `ActingAsToken` and `ActingAsSession`
  issue real credentials through the configured token/session bindings.
- `testkit/dbassert`: `AssertExists`, `AssertMissing`, `AssertCount` and
  `AssertSoftDeleted` over typed generated queries.
- `testkit/factory`: per-call overrides (`DraftWith`, `CreateWith`),
  `AfterCreating` hooks, `CreateFor`/`CreateHas` relationships and
  `NewFaker(seed)`, a deterministic standard-library generator of fictional
  names, emails, words, sentences, numbers and times.

#### Performance

- Generation lists the selected packages once without compiling them and requests
  compiled export data only for dependencies outside the selection, reuses the
  recovery walk of the source tree, skips `node_modules`, `vendor`, dot/underscore
  directories and `go.mod` `ignore` entries, and analyzes independent packages
  concurrently with bounded workers while reporting errors in dependency order.
  A warm `generate --recursive --check` of the consumer fixture (134 packages,
  200 generated files, isolated copies, five alternating runs on this
  workstation) took 1.07–1.08 s before and 0.59–0.65 s after.

### Earlier unreleased changes

- Outbox publishers normalize their own SQL timestamps to microseconds, including
  empty eligibility polls and successful completion. Retry deadlines round up
  after adding the full delay to the original clock sample, preventing early
  retries with nanosecond clocks or fractional-microsecond delays. Generic clocks,
  temporal values and strict caller-value SQL codecs retain their contracts.

- Workers log safe structured failure metadata through the configured logger by
  default. Typed `application.JobWith` adds middleware/admission to ordinary
  assembly. Failed-job commands inspect metadata and explicitly retry independent
  failures using a saved concurrency token. Memory/Redis retries retain job IDs,
  envelopes and bounded history, deduplicate repeated requests, and preserve
  custom backend compatibility through the optional `jobs.RetryBackend` interface.
  Lease loss now stops other reservation loops immediately while retaining
  ownership of handlers that are still draining after cancellation.

- Typed pagination now composes with concrete guard bindings through
  `pagination.Authenticated` for numbered, simple and cursor reads. Scope,
  permission and request authorization retain the HTTP lifecycle; page completion,
  links, errors and exported security reuse their existing owners.

- Private configured consumer packages now include the localization declarations
  and catalog assets required by their configured translation tests.

- Browser validation now retains its English fallback for plural arguments with
  more than 20 fractional digits, avoiding incorrect forms caused by JavaScript
  plural rounding. Exact Go plural selection remains unchanged.

- Named database validation now has explicit consumer guidance and PostgreSQL
  coverage for concurrent built-in/custom checks, selected-connection failures,
  cancellation and pool reuse. Prepared translation recipes also have fuzz
  coverage for serialization, bounded rendering and literal fallbacks.

- Validation messages now share typed recipes, English fallbacks, field/comparison
  labels and plural bounds. Configured locale-enabled HTTP applications render
  request-locale messages automatically, including decoder diagnostics, without
  changing response codes or paths. Typed `WithTranslation`, literal overrides,
  explicit `Errors.Localize` and generated client presentation are supported.

- Validation adds common text, identifier, numeric, consent, comparison,
  collection and password-strength rules, including redacted password inputs.
  `Parallel` defaults to four concurrent branches with shared work limits,
  ordered diagnostics and owned cancellation. Typed model `ExistsAll` batches
  scoped database observations without per-item round trips. Existing sequential
  rules and database constraint/authorization responsibilities are preserved.

- Application `TimeZone` defaults to UTC and binds `s.Time()` date helpers,
  `s.Calendar()` schedules, log timestamps/daily rollover and export presentation.
  Typed TOML/environment/override settings and per-service overrides are supported.
  IANA timezone data has an embedded fallback. Calendar helpers preserve strict
  DST errors; database instants and temporal JSON remain UTC.

- File log sinks now rotate automatically on a new configured-zone day or before exceeding
  20 MiB, retaining at most 14 archives for up to 14 days. Typed rotation settings
  support TOML, environment inputs and generated overrides; an explicit disable
  restores externally managed append-only behavior. macOS/Linux file locks reject
  competing rotation owners. Startup/rollover/hourly-on-write cleanup only removes
  recognized archives. The application default remains INFO JSON on stderr.

- Team adoption now documents dependency installation, a module-pinned CLI,
  generation, explicit migrations and application CI. The independent consumer
  pins `go tool foundry` to its runtime dependency and checks real doctor/generation
  commands. Empty-project instructions include the required Go package initialization.
  Stale pending notices now link to delivered acceptance evidence.

- Security hardening adds a shared `no-store` default for cookie authentication,
  optional routes and failures; typed outbound destination policies enforce
  address-checked direct connections; inbound Standard Webhooks/Stripe verification
  preserves signed bytes and typed account/delivery identity before idempotency.
  PostgreSQL migrations can explicitly run nontransactional statements with durable
  checkpoints and locked reconciliation. Existing transactional checksums remain
  compatible. Repository CI/security gates and private-reporting policy are added.
  Final verification and re-audit corrections are accepted; see the
  [acceptance report](docs/guides/security-hardening-20260924.md) and
  [continuation](blueprint/security-hardening/README.md).

- Database cursor codecs and custom model transactors use bounded error inspection.
  Unreadable transaction outcomes retain fully hydrated reconciliation candidates
  without returning a successful write or changing cursor failures into input errors.

- Authentication bounds password-lockout, recovery-issuer and MFA rejection error
  inspection. Cyclic failures release operation capacity, grant no authority and
  preserve recovery codes; admitted recovery requests keep their uniform response.

- Module review: entry-point docs now lead with configured application bootstrap
  and the integrated typed API workflow. Named/default selection errors identify
  the generated setting path without formatting configured values. Error classification
  bounds cyclic/deep/wide chains so observation spans and CLI reporting can finish.
  Outbox publication reuses bounded, isolated inspection and retains retries when
  an error cannot be safely classified. Database callback inspection also rejects
  unbounded graphs before adapter classification so rollback can release its pool.
  Pub/sub cancellation inspection uses the same bounds so failed streams can close.
  TypeScript export/runtime now support valid explicit map schemas without a key
  constraint, retaining their concrete value types and wire bounds.
  Scheduler completion also bounds handler/hook error traversal so capacity and
  overlap leases are released after a cyclic failure. Post-verification audit also
  bounds outbox shutdown/observer errors and scheduler lease-error inspection,
  preserving committed publication and releasing scheduler capacity.
  Worker admission, handler and backend error searches are bounded too, preserving
  retry budgets, reached permanent markers and draining shutdown.
  Email classification also bounds error traversal, preserving conservative retry
  decisions and releasing send capacity after cyclic driver/attachment failures.
  Notification recipient inspection has the same bounds; failed lookups retain
  retryable channel state without rendering or submitting.
  Attachment cleanup and transaction outcome inspection are bounded, preserving
  published replacements, pending cleanup and unknown transaction outcomes.
  Storage outcome inspection is bounded and performed once, preserving reached
  mutation outcomes and cleanup references through late cancellation. Adapter and
  HTTP download inspection is bounded too; malformed upload errors still run
  multipart abort cleanup.
  WebSocket error classification is bounded as well, so cyclic handler, room codec
  and cluster adapter failures cannot trap connection or shutdown ownership.
  HTTP response, authentication and retry searches also use bounded inspection;
  unknown cyclic failures become safe internal errors without retaining requests.
  Cursor and custom asset searches are bounded; asset classification also owns
  panic/Goexit. Compression and ETag diagnostics no longer format custom I/O errors.
  Stale pre-acceptance guide notices now link to the authoritative roadmap.
  The complete module review, post-verification audit fixes, final native gate,
  affected races, typed consumers/clients/editor checks, fuzzing and private
  package/security review passed; see the [review record](docs/guides/module-review-20260923.md).

- T07 source batch adds an integrated public consumer for nested authenticated
  PATCH, generic/tagged responses, typed pagination, shared input rules and
  transaction-bound submissions with duplicate-safe named receiving effects.
  Native full verification, PostgreSQL/races, strict clients, packaged consumers,
  compiler/editor checks, fuzzing and the complete final audit passed. Audit
  fixes preserve named generic byte-slice base64 schemas, count TypeScript object
  names in JSON node limits and invalidate child acceptance on `.mjs` changes.
  Shared fixture authentication and generated draft presence remove duplication.

- T06 adds typed transaction-bound inbound idempotency, exact HTTP
  outcome capture, current authorization on replay, bounded PostgreSQL admission,
  explicit retention maintenance and manifest v4 client contracts. Native full
  verification, PostgreSQL races, actual process-crash recovery, typed clients,
  compiler/editor checks, bounded fuzzing and repeated cost measurements passed.

- T05: typed PostgreSQL schema scopes restore every pooled checkout; retained
  test namespaces configure named/default/read pools, shared feature migrations
  and complete HTTP applications with real commits. Added a bounded client for
  production test HTTP kernels. Native full verification, PostgreSQL/race,
  compiler/editor acceptance and scoped-checkout cost checks passed.

- T04 implementation: unique alternate-key lookups, typed nested model bundles,
  declared parent relationship scoping and post-binding resource authorization.
  Existing query filters, soft deletion, hooks, eager loading, caller transactions
  and HTTP contracts are retained. Native verification, PostgreSQL/race integration,
  compiler/editor acceptance and deterministic generation passed.

- T03 implementation: typed URL-encoded forms, generated fields, independent form
  limits and request preparation/authorization with concrete guard actors. Manifest
  format 3, OpenAPI and TypeScript retain exact form cardinality/media and defer
  business validation until server preparation. Native verification, races, consumer/
  client/compiler/editor acceptance, fuzzing and hook-cost measurements passed.

- T02: generated closed tagged payload unions with typed constructors, owned
  accessors and complete visitors. Runtime/schema validation, manifest format 2,
  OpenAPI and TypeScript share variant declarations. Native verification, races,
  compiler/editor/client acceptance, fuzzing and cost measurements passed. Nested
  codec failures retain safe internal classification and payload byte bounds.

- T01: generated generic DTO contracts and typed validation fields, preserving
  concrete handler/client types, collections, presence and nullability. Native
  verification, races, compiler/TypeScript/gopls acceptance and cost measurements
  passed. Native byte-slice composition now preserves its base64 wire shape and
  rejects incompatible element restrictions.

- Added the Go source audit and T01–T07 typed API continuation blueprints for
  generic DTOs, payload unions, forms/request hooks, nested binding, isolated HTTP
  tests and inbound idempotency, with integrated acceptance and final re-audit.
  These are planned contracts; no new runtime behavior is delivered by this entry.
  New work uses Foundry-Go source and tests without sibling-repository references.

- Performance/security review fixes: persistent cache expiry is evaluated after
  lock acquisition, full memory caches skip expiry scans until an expiry can be
  due, and outbound streams reject repeated empty reads through the shared
  progress guard. Full native verification, broad races, 52 fuzz targets, same-host
  benchmarks, independent consumer measurements and the completion audit passed.

- Consumer startup audit aligns both serve commands with graceful interruption and configured shutdown waits while retaining cleanup failures.

- Fixed model-extension module startup rejecting its own shutdown resource name. Configured consumer acceptance now exercises the module lifecycle.

- Consumer startup C05: compact executable consumer, config-only cache/storage switching proofs, configured native measurement profile and private format-2 packaging. The post-audit native gate, packaged consumers, repeated runtime/editor measurements and security review passed.
- Audit scopes now resolve as concrete `audit.Scope` handles through `services.AuditScope()`, so domain constructors can retain them and call `Within` after their resolver seals.
- Consumer startup C04: named mail/jobs/HTTP/logging/pub-sub/realtime, typed worker/scheduler/channel declarations, persistent actor helpers, configured feature managers and explicit migration targets. The final native gate and targeted races passed; final consumer/audit acceptance also passed in C05.

- Consumer startup C03: configured application/HTTP assembly, owned JSON sinks, request completion observations, typed guard bindings and executable web acceptance fixture (verification status is owned by the master blueprint).

- Consumer startup continuation: detailed blueprint contracts and C01 source for
  generated typed configuration keys/schemas, nested groups, scalar/enum/text
  codecs and bounded TOML file loading. C01 passed its full native gate, races,
  independent consumers, compiler rejection and real editor acceptance. Configured
  service assembly remains subsequent continuation work.

- Documented the clarified Laravel-like, fully typed consumer direction and a
  source audit of configured assembly, named services and automatic defaults.
  The audit separates proposed improvements from accepted component delivery;
  PostgreSQL remains the only database adapter in scope. Corrected reviewed
  guides and README statements that still described accepted features as pending.

- Final audit fixes verified: owned inspection of database,
  storage, worker and pub/sub errors; worker backend panic/Goexit isolation;
  lifecycle cancellation inspection outside the application mutex; conservative
  lease cleanup; checked route-factory transport types and signing; and repeatable
  field-documentation recovery after a second interruption. Regression sources
  cover ownership, rollback, redaction and factory/recovery boundaries.

- Milestone 24 accepted: generated binary fields with owned draft/query/change
  buffers; optional PostgreSQL read routing with explicit primary reads, endpoint
  health, combined connection bounds and coordinated shutdown; shared bounded
  observations, trace/error exporters, protected diagnostics, readiness and
  maintenance across kernels; native HTTP connection/request ceilings and
  explicit versioned queued/outbound trace propagation. The source batch also
  includes independent module packaging, controlled consumer/editor measurements,
  agent timing metadata, compatibility/runbooks and the approved MIT license.
  Consolidated native verification, PostgreSQL/Redis, compiler/editor/TypeScript
  gates, races/fuzz, independent packaged consumers, vulnerability/license review
  and the final complete-framework audit passed. Measured ordinary/full cold
  builds took 3.30/60.24 seconds on the recorded 64 GiB native Mac.

- Milestone 23 accepted: typed application
  commands, artifact scaffolds, offline doctor and metadata inspection; owned
  HTTP/realtime clients, typed factories and explicit local capability helpers;
  typed PostgreSQL planning/execution analysis. Compiler cases share bounded Go
  batches and each editor scenario shares one fresh server. External-child test
  inputs receive a source/tool fingerprint while ordinary caching remains enabled.
  Native full verification, independent consumer/plugin modules, PostgreSQL/Redis,
  compiler/editor/TypeScript checks, relevant races and bounded plan fuzzing passed.
  Verification corrected an expected compiler diagnostic and a race-test process
  timeout; review added consistent group help and complete child-cache tracking.

- Milestone 22 accepted: typed plugin manifests and dependency lifecycle, direct
  contributions and explicit overrides, namespaced configuration, historical
  migrations and owned asset/scaffold distribution through shared recovery.
  Native full verification, independent plugin/consumer modules, PostgreSQL
  migration history, worker/event/route use, compiler/editor checks, races and
  bounded manifest/path fuzzing passed.

- Milestone 21 accepted: shared versioned contracts, OpenAPI 3.1.1 export and
  typed HTTP/realtime TypeScript clients with lossless wide numeric codecs,
  declared validation, multipart/download ownership and bounded protocol handling.
  Publication shares generator ownership/check/recovery. Native full verification,
  strict TypeScript, real HTTP/WebSocket interoperability, consumer/compiler/editor,
  races, bounded fuzz and additive older-client compatibility passed. Verification
  corrected embedded metadata inspection, empty transport lists, macOS publication
  paths, cleanup error preservation and synchronous/asynchronous callback isolation.
- Milestone 20 accepted: immutable localization catalogs, generated typed message
  arguments, exact cardinal/ordinal operands and shared enum/validation/permission
  labels. Named outbound HTTP clients add bounded pooling, operation ownership,
  explicit safe retries, callback-scoped streams, generated DTO codecs and fakes.
  Existing credential entropy and email transports share their implementations;
  focused collection/temporal helpers and established HTML sanitization complete
  the supporting APIs. Native full verification, consumer/compiler/editor,
  generation, race and bounded fuzz checks passed. Verification corrected large
  plural operands and fresh message-generation dependency discovery.

- Milestone 19 accepted: typed datatables with generated projection/DTO contracts,
  scoped list/count/export pipelines, joined/grouped and relation filters, literal
  case-insensitive query comparisons, bounded CSV/XLSX artifacts and existing
  HTTP/jobs/storage composition. Native full verification, public consumer,
  compiler/editor/generation, race, fuzz and streaming-memory checks passed.
  Verification corrected immediate shutdown cancellation observation and an
  XLSX escape-prefix/carriage-return text corruption found by fuzzing.
  Queued delivery retains the export deadline/ownership context and propagates
  cancellation while preserving the job execution identity.

- Milestone 18 accepted: bounded immutable image plans, eight output formats,
  typed attachment collections, durable publication/cleanup recovery, deliberate
  retention, localized batches and existing jobs/outbox reconciliation. Shared
  typed owners support metadata, translated fields, settings and explicit
  versioned country seeding. Native full verification, focused races, public
  consumer, compiler/editor/generation and image fuzz checks passed. Review
  hardened embedded image/intermediate limits, large translation cleanup,
  UUID orphan pagination, shared borrowed-reader progress handling, and
  same-file generator source locations after managed field notices.

- Milestone 17 accepted: typed notification/recipient declarations, persistent
  per-channel delivery, scoped inbox/read operations, frozen email output,
  private realtime rooms and existing jobs/outbox composition. Provider lookup,
  eligibility and shared transaction scoping remain single sources of truth.
  Native full verification, focused races, PostgreSQL rollback/deduplication,
  WebSocket ownership, consumer/compiler/editor/generation and snapshot fuzzing
  passed. Review fixed render-time revocation, concurrent preparation rejection
  and stable provenance across repeated outbox publication. No new dependency.

- Milestone 16 accepted with real-provider smoke gap: typed email
  templates/messages, bounded storage attachments/MIME, SMTP and Mailgun,
  Postmark, Resend and SES adapters, borrowed transport lifecycle, safe notices,
  memory/log drivers and existing jobs/outbox integration. Added terminal job
  failures, accepted-side-effect retry prevention and owned error classification.
  Native full verification, root/consumer races, local SMTP/TLS/STARTTLS, provider
  HTTP fixtures, real PostgreSQL rollback/commit, compiler/editor/generation and
  bounded address fuzzing passed. No new dependency; real-account email smokes
  remain unverified.

- Milestone 15 accepted: Redis WebSocket fan-out,
  bounded replay/live deduplication, typed relay/acceptance, TTL presence, cluster
  limits, typed revocation, heartbeat/fresh authorization, bounded shutdown drain
  and protected per-channel diagnostics. Native full verification, root/consumer
  races, real Redis two-server acceptance, 832 compiler rejections, 319 catalogued
  editor scenarios and parser fuzzing passed. Review fixed backend/stream-cleanup
  callback self-wait protection and made replay/live overlap testing deterministic.
  No new dependency.

- Milestone 14 accepted: typed WebSocket channels,
  room/event/payload ownership, strict versioned frames, local publication,
  per-operation authentication, owned user rooms, safe presence DTOs and native
  HTTP/WebSocket kernel integration. Native acceptance, races, 42,636 parser fuzz
  executions, 827 compiler-rejection cases, 319 real-gopls probes and generation
  passed. Review strengthened unsubscribe/disconnect and blocked codec ownership
  regressions.

- Milestone 13 accepted: parsed cron/anchored intervals with
  explicit timezone/DST behavior, bounded catch-up/concurrency/history, shared
  lease leadership/overlap protection, owned hooks/shutdown, typed job targets
  and Scheduler module. Native acceptance, races, real Redis failover/overlap,
  821 compiler-rejection cases, 316 real-gopls probes and generation passed.
  Review also isolated abnormal custom error inspection and kept domain errors
  out of lease coordination classification.

- Milestone 12 accepted: typed job dispatch/capture, bounded memory and
  atomic Redis queues, owned workers, middleware, shared rate limiting, uniqueness,
  chains/batches and bounded operational history. Transactional job enqueue and
  shared outbox publication use stable execution IDs; existing durable events can
  deliver through workers with typed outbox identities. Application modules,
  consumer examples and failure tests passed native acceptance, required local
  PostgreSQL/Redis, races, 818 compiler-rejection cases, 313 real-gopls probes and
  current generation. Review fixes preserve batch ownership during cancellation,
  retired-workflow capacity and scheduled timestamp boundaries.


- Milestone 11 accepted: native checks plus live local/AWS S3/R2 storage behavior,
  consumer contracts and cleanup verified. Live AWS testing found and fixed
  form-encoded listing keys: spaces, literal plus signs, percent sequences and
  Unicode now retain exact identity through object/upload pagination and version
  cleanup. AWS historical-version reads and conditional deletion passed; public
  AWS URL testing remains optional for private buckets.

- Storage reader close now cancels the backend context before interrupting an
  active read, preserving cancellation reliably during reader or disk shutdown.

- Live R2 storage checks passed, including multipart abort cleanup and signed/public
  URL payload integrity. Fixed R2 upload-generation headers being mistaken for
  historical versions. Added explicit NFC key/prefix restrictions to prevent R2's
  provider normalization from making distinct spellings address the same object.
  Local/AWS preserve byte-exact names. Separate AWS certification remains pending.

- Milestone 11 native checks passed: typed storage/lifecycle, managed local
  publication/cleanup, official SDK S3/R2 streaming/multipart/signing and HTTP
  file integration. Storage/consumer races, typed compiler/editor checks, fuzzing
  and generation passed. Review fixes preserve known sizes for conditional
  file/copy helpers and classify provider metadata failures accurately. Real AWS/R2
  certification remains pending; see the [storage guide](docs/guides/storage.md).

- Milestone 10 accepted: typed authentication, scopes/permissions, session/token security, password recovery, MFA, attribution, signed/model-bound/native HTTP, security-event outbox and retirement. Complete native checks passed, including local PostgreSQL/Redis, race tests, 805 invalid API cases, actual gopls, current generation and parser fuzzing. Earlier unverified entries below are historical implementation notes.

- Added typed MFA transactional/rejection observations, event-outbox consumer composition, administrative retirement in the caller's transaction, and credential maintenance guidance. Hardened recovery callback ownership against suppressed failures. Source parity review is complete; consolidated milestone 10 verification/fixes are next, and these additions remain unverified.

- Added unverified required/optional authentication composition for signed DTO
  endpoints, model-bound resources and native HTTP handlers. Shared admission
  retains model types, permission/scope checks, attribution and scope cleanup;
  signatures reuse existing transport validation. New acceptance sources await
  milestone 10 verification.

- Added unverified `testkit/auth` helpers for test-owned production scopes and
  concrete guard assertions, preserving normal verifier/provider/policy behavior.

- Added unverified typed recovery-state revisions, using the existing UUID codec.
  Reset/verification bind the stored email generation; consumer model hooks rotate
  it on address changes and clear verification, including restored old addresses.
- Added unverified `Guard.Origin`/`WithAttribution` and automatic attribution in
  typed HTTP authentication, reusing the verified identity and request model cache.
- Added unverified model-owned `Permission` declarations through the existing policy
  runtime, HTTP permission requirements and owned route metadata. Runtime,
  PostgreSQL, consumer, compiler and editor test sources await the milestone gate.

- Added the first model-first authentication slice: typed providers/strategies/guards,
  request-owned resolution, optional authentication, concrete policies and guarded
  HTTP adapters. Focused race, consumer, compiler and gopls checks and full native
  regression passed; the concrete consumer experience was reviewed. Session/token stores and password/MFA flows remain ahead.

- Added an explicit scoped Redis command/script API with immutable arguments, typed decoders, heterogeneous pipeline results, shared reply bounds and no mutation retries. Typed data handles can explicitly export their resolved adapter key. Focused behavior/races, consumer/compiler/editor checks and full native verification passed. Milestone 09 passed its source/parity closure review.

- Added typed Redis hash/set declarations, owned JSON results, cardinality limits, explicit expiry retention and bounded atomic data deletion. Native behavior, race and consumer/compiler/editor checks and full native verification passed.

- Typed cache and counter handles now provide `Exists`, `Expire` and bounded atomic `ForgetMany`. Memory and Redis share namespace/tag protection, validate the complete batch before deletion and preserve payloads during TTL changes. Focused native races, consumer/compiler/editor checks and full native verification pass.

- Cache `Store.Invalidate(ctx)` invalidates the complete typed-cache namespace through automatic reserved snapshots, including tags, counters and local/distributed fills. Native memory/Redis reuse stable addresses and existing atomic version checks. Callback ownership, complete milestone 09 races, consumer/editor checks, affected compiler cases and full native verification with local PostgreSQL/Redis pass.

- [Typed pub/sub](docs/guides/pubsub.md) adds model-owned resource keys, versioned JSON payloads, bounded subscriptions, explicit loss, and memory/Redis adapters. Redis owns acknowledged setup, dedicated connection limits, heartbeat and draining shutdown. Focused native races, consumer/compiler/editor checks, bounded queue fuzzing and full native verification with local PostgreSQL/Redis pass.

- [Typed rate limiting](docs/guides/rate-limiting.md) adds model-owned quotas, explicit memory and atomic Redis authorities, and HTTP 429/503 integration. Focused native races, consumer/compiler/editor checks, timestamp fuzzing and full native verification with local PostgreSQL/Redis pass.

- [Distributed Remember](docs/guides/distributed-cache.md) retains typed cache APIs while sharing renewable fill ownership across instances. Atomic proof/tag checks prevent stale publication; errors do not cause fallback or mutation retries. Focused native races, consumer/compiler/editor checks and full native verification with local PostgreSQL/Redis pass.

- [Typed leases](docs/guides/leases.md) preserve resource key types and own bounded acquisition, heartbeat, cancellation on ownership loss, conditional cleanup and reverse shutdown. Memory and Redis adapters, consumer/compiler/editor checks, focused races and full native verification pass. Release preserves a manager cancellation that occurred before explicit cleanup. Shared keyspace primitives preserve existing cache APIs and physical addresses.

### Changed

- Redis now implements typed cache tags and tagged counters with atomic metadata batches, stable payload addresses, fresh versions after metadata loss, and stale-writer protection across clients. Shared contracts, native failure tests, consumer/editor checks and full native verification with local PostgreSQL/Redis passed.

- The approved go-redis adapter now owns bounded standalone connections and application lifecycle, and supplies the existing typed cache and exact counter APIs. Shared memory/Redis contracts, transport failure fixtures and full native verification with local PostgreSQL/Redis passed. Redis tags and distributed coordination remain required.

- Independent editor acceptance probes now run with a maximum of four concurrent gopls sessions, retaining their completion, hover, definition and source-integrity assertions.

- Local framework development now uses native macOS Go tools with the repository-selected toolchain. Historical VM verification records are retained; Go commands no longer require a VM or SSH bridge.

### Verification

- Milestone 08 is complete. The full canonical regression including gzip/Brotli compression passed with required PostgreSQL, complete actual-gopls coverage, all 641 compiler-rejection cases, current generated output and documentation checks. Independent consumer examples were reviewed, and older HTTP guides now describe the delivered APIs. Remaining framework milestones and the final framework audit are still required.

- The full canonical regression through static/SPA passed with required PostgreSQL, complete gopls, all 638 compiler-rejection cases, three generation-freshness targets and matching source fingerprints. Automatic ETags subsequently passed focused and full canonical acceptance with all 639 compiler-rejection cases. Compression and milestone 08 completion remain required.

- Typed downloads passed canonical HTTP/consumer races, six compiler cases, three real-gopls probes, vet, formatting, generation freshness and documentation checks. Combined regression including subsequent stream integration also passed.

- The combined transport `make verify` gate passed with real PostgreSQL and gopls, 614 compiler-rejection cases, vet, formatting, three generation-freshness targets and documentation checks. Multipart/download drafts were outside that snapshot; milestone 08 and the rest of the framework remain in progress.

### Fixed

- Cancellation protocol tests retain immediate framework-ownership checks and verify background SQL connection cleanup within a bounded deadline. Both affected scenarios passed 100 race-enabled repetitions; native PostgreSQL and database runtime race suites also passed.

- Generator fixtures now use canonical temporary paths and inherit approved dependency requirements/checksums from the framework module. This fixes macOS path-alias checks and fresh HTTP fixture loading after the Brotli dependency was added.

- Language tooling drains final server output during shutdown, preventing a responsive gopls from blocking on a full pipe. A reproducing protocol regression, race checks and real-gopls probes passed; the existing shutdown bound and caller cancellation remain unchanged.

- Formatting commands share a source scan that excludes private caches and dependency trees while retaining consumer fixtures and paths containing spaces; a disposable fixture verified exclusion, rejection and write/check behavior.

- Optional and nullable JSON fields preserve exact dynamic numbers through nested wrappers, HTTP DTO decoding and stored JSON snapshots. Regression tests reproduce the previous rounding; runtime/consumer races and affected vet passed.

- SQL snapshots and custom codec binding preserve nil byte slices as SQL NULL, keep empty bytes non-null, and reject NULL model identities; audit snapshots and change tracking retain the same distinction.

- Stored model-reference codec errors retain their causes for `errors.Is` while keeping key/value details out of ordinary diagnostic messages.

- Generated draft defaults avoid shadowing handwritten field types and imported input packages by using the existing identifier allocator.

- Test gates share an explicit per-package timeout, allowing the complete serial gopls suite to finish while preserving individual operation deadlines and failed-batch stopping.

- Constructor `runtime.Goexit` now reports a build failure and expires retained resolvers, including typed contribution lookup, through the existing callback-isolation helper.

- Upsert conflict literals use the destination field's automatic mutator and final codec validation. Mutated fields reject SQL calculations/cross-field copies that would bypass that behavior; their own proposed values are copied without a second transformation.

- Language tooling allows a bounded grace for slow gopls shutdown, preserves caller cancellation, and identifies cleanup failures separately from inspection responses.

- Generator rollback/recovery fixtures snapshot actual file permissions, preserving their intended failure checks under restrictive creation masks and verifying restored permissions.

- Row-lock validation finds window functions nested in calculations, CASE conditions and ordering, while preserving independent scalar-subquery SELECT scopes.

- Make verification stops after the first failed package batch, preserving complete package coverage on successful runs and avoiding further builds after resource or test failures.

- Consumer compiler-rejection tests cancel their owned process group on POSIX systems, preventing timeout-driven compiler leaks; inherited output pipes are bounded on other platforms.

- Verification runs all discovered packages in bounded batches, reducing temporary build disk requirements while rejecting failed or empty package discovery.

- Nested Unix-millisecond conversions compile the input once through exact PostgreSQL integer interval parsing; scalar SQL expansion has a bounded compilation-work budget.

- Database codecs preserve the difference between a non-nil empty byte buffer and nil while copying custom encoder output.

- Empty `In()` predicates discard bindings belonging to their omitted operand while still validating it, preserving surrounding placeholder numbers for computed expressions and filtered aggregates.

### Added

- Added unverified typed password-reset/email-verification JSON requests, protected HTTP completion, and framework-owned recovery link requests with recipient quotas, bounded callbacks, stored-address delivery and generic public acknowledgements. Canonical named-generic contracts preserve model/purpose types in imported and executable consumers. Credential request protection reuses the secret-response HTTPS/POST boundary. Consumer generation succeeded; new behavior, security, compiler and editor tests await the milestone gate.
- Added unverified model-bound MFA completion for sessions/tokens: one current-model lock, provisional challenge consumption, factor replay state and full credential creation share a transaction. Browser completion preserves cookie/CSRF response ownership; typed enrollment/recovery response adapters share secure POST/no-store handling with token delivery. Generated input contracts preserve enrollment model ownership. New behavior, PostgreSQL, compiler and editor tests are written for the milestone gate.
- Implemented typed [MFA factor management](docs/guides/mfa.md), encrypted PostgreSQL persistence and explicit migrations. Enrollment/confirmation, disable, recovery regeneration and key rotation share model-first locking; confirmation and security changes join credential revocation. Lockout finishes before protected writes. Consumer, rollback/concurrency/security and compiler/editor tests are written but unrun. Single-use pending credential completion and typed HTTP delivery are now written; milestone acceptance remains open.

- Added shared authenticated encryption and typed TOTP/recovery primitives with generated input contracts. Password model rechecks reuse issuance validation while preserving the original provider and model types. Consumer, security and compiler/editor tests are written and unrun; transactional MFA flows remain in progress. See [MFA](docs/guides/mfa.md) and [encryption](docs/guides/encryption.md).

- Added the unverified `auth/password` core with distinct redacted plaintext/hash values, bounded Argon2id work, canonical PHC parsing, constant-time checking, rehash policy and generated login DTO support. The approved Go x/crypto dependency and required indirect updates are installed; tests await the milestone gate.

- Typed token HTTP delivery and refresh bodies now preserve model/key ownership, use generated wire metadata, enforce secure POST/no-store delivery and keep ordinary credential serialization redacted. Consumer, security and compiler/editor tests are written; execution remains deferred to milestone 10 completion.

- Implemented [typed token persistence](docs/guides/tokens.md), bounded refresh families, replay revocation and generated PostgreSQL storage. Shared credential hashing/address/callback ownership with sessions. Tests and consumer fixtures are written; verification is deferred to milestone 10 completion under the updated cadence.
- Added [model-owned access scopes](docs/guides/access-scopes.md), immutable scoped proofs and guard/HTTP requirements sharing one model lookup. Session issuance rejects scoped proofs to prevent discarding restrictions. Focused checks passed; full regression will run at milestone 10 completion. Token persistence and refresh remain.
- Added [typed browser sessions](docs/guides/browser-sessions.md): login/rotation/logout in ordinary DTO handlers, staged cookies, native CSRF protection and strict origin fallback. Public auth errors now share HTTP 401/403 contracts while explicit domain errors retain their declarations. Focused native races and full regression passed in 503.3 seconds, including 721 compiler cases and 271 editor probes.


- Added [typed session persistence](docs/guides/sessions.md) with PostgreSQL authority, hashed credentials, atomic rotation, idle/absolute expiry, model-owned listing/revocation and bounded maintenance. Focused native races and full regression passed, including all 717 compiler cases and 269 editor probes. HTTP session/CSRF integration is recorded above.


- Typed cache tags preserve key ownership through references and invalidation. Stable data keys and version fingerprints prevent stale-writer replacement and metadata-loss resurrection; tagged CRUD, Remember and counters share the existing cache runtime and bounded memory storage. Redis integration remains required.
- Approved go-redis v9.22.0 is installed for the upcoming native Redis adapter; existing dependency versions and the Go requirement were preserved.
- Typed atomic cache counters preserve model-owned keys and exact int64 values, share declaration/storage ownership, retain initial expiry and reject overflow or corrupt data without replacing the existing entry. The memory implementation passed focused races, consumer/compiler/editor checks, 619,028 arbitrary-precision arithmetic fuzz cases and full native verification; Redis acceptance remains required.
- Typed cache `Remember` coalesces misses within a store, preserves key/loader/result types, bounds active fills and waiters, isolates snapshots, and handles cancellation and callback failures without detached work. Focused races, compiler/editor checks and full native verification passed. Distributed coalescing remains in milestone 09.
- [Typed caching](docs/guides/caching.md) binds model-owned keys and concrete payloads to reusable declarations, with explicit TTLs, miss/error separation and a bounded memory adapter. Focused races and full native verification passed for the first slice, including consumer/compiler/editor acceptance; Redis and the rest of milestone 09 remain required.

- [Response compression](docs/guides/http-compression.md) provides typed gzip/Brotli configuration, bounded streaming and encoder concurrency, negotiated errors, conditional metadata, and shared native response controls. Interrupted sources cannot become successful encoded responses. Focused runtime/consumer races, compiler and actual-gopls checks, fuzzing, allocation samples, vet, current generation and docs passed. Subsequent full canonical regression completed milestone 08.

- Automatic ETags use typed configuration, bounded capture, native conditions and shared response controls around native handlers and typed DTOs. Field getters remain explicit in DTO mapping; stored models are preserved. Overflow, flush, source failures and full-duplex transfers retain documented ownership. Focused and full canonical acceptance passed, including all 639 compiler-rejection cases and current generation. Compression integration subsequently passed full regression.

- [Static assets and SPA routing](docs/guides/http-assets.md) provide framework-owned local/embedded sources, typed mounts and URLs, native conditions/ranges, confined local paths and bounded streaming. SPA fallbacks preserve API errors, method handling and separate application prefixes. Focused runtime/consumer races, five compiler cases, three real-gopls probes, fuzzing, benchmarks, vet, formatting, generation freshness and documentation checks passed. Full canonical regression also passed.

- [Typed unseekable stream responses](docs/guides/http-streams.md) retain concrete handler/source contracts, optional exact lengths, shared media metadata and bounded output. EOF is checked before sending the final declared chunk; interrupted transfers abort without exposing internal errors. Runtime/consumer races, six compiler cases, three new actual-gopls probes, fuzzing and resource checks passed. Complete combined regression also passed.

- Typed download responses retain concrete file results and declared media, use confined local sources, support native HEAD/conditions/ranges, share 412/416 error contracts, and own cleanup after cancellation or failed transfers. Root/consumer races, six compiler cases, three real-gopls probes, range fuzzing, streaming benchmarks and freshness checks passed. Canonical acceptance and combined full regression also passed.
- Typed multipart uploads generate concrete form fields and validation selectors, retain exact text/file/JSON types, stream to request-owned temporary files, and clean up retained readers after response writing. Runtime/race, generator, consumer, seven compiler-rejection cases, three actual-gopls probes, fuzzing, bounded allocation benchmarks and freshness checks passed. Canonical acceptance and combined full regression also passed, with real PostgreSQL/gopls, all 621 compiler cases, current generated output and matching source fingerprints.

- Typed route-model binding passed focused runtime, generated-consumer, compiler and real-gopls acceptance, plus isolated PostgreSQL tests. Generated primary-key queries retain exact key/model types, query scopes, soft-delete visibility, eager loading and retrieval hooks. The adapter resolves once per request and preserves explicit transport DTOs. Combined transport full regression passed.

- Typed cursor HTTP pagination passed focused runtime, consumer, compiler, real-gopls and freshness acceptance. It preserves source-owned cursor positions through explicit DTO mapping and reuses the shared endpoint, validation, codec and URL contracts. Invalid cursor input remains distinct from server/codec failures. Combined transport full regression passed.

- Typed numbered/simple HTTP pagination reuses ORM page requests, generated filters, explicit getter-to-DTO mapping and shared endpoint contracts. Framework-owned metadata and approved-origin navigation reject inconsistent results before success output. Cursor HTTP adapters and combined transport acceptance remain in milestone 08.

- Typed query composition and defaults passed focused runtime, consumer, compiler and actual-gopls acceptance. Generated filters can be embedded and combined without duplicating parsing or scalar metadata. Typed defaults preserve omission, explicit zero/false/empty values and owned per-request results; canonical default URL metadata shares runtime declarations. Combined transport full regression passed.

- Typed application HTTP errors passed focused runtime, consumer, compiler and actual-gopls acceptance. Immutable declarations own public code, status and message metadata; typed endpoints share those declarations with response classification and contract inspection. Private causes retain ordinary Go error identity. Invalid declarations, undeclared failures and conflicting catalog entries are covered. Combined transport full regression passed.

- Native streaming JSON value contracts passed focused runtime, generation, consumer, compiler and actual-gopls acceptance. Typed JSONContract methods now admit native JSONTo/JSONFrom codecs through shared capability checks. Native precedence, fallback, singular consumption, exact numbers, callback ownership and partial-value rejection are covered. Combined transport full regression passed.

- Generated URL source identities passed focused runtime, executable, consumer, compiler and actual-gopls acceptance. A real main executable reproduced the previous URL/DTO identity mismatch before the fix. Generated URL, JSON value and map-key metadata now share source type names, including model-ID type arguments, while preserving native codecs and scalar shape. Combined transport full regression passed.

- Typed JSON map-key contracts passed focused runtime, fresh-generation, consumer, compiler and actual-gopls acceptance. Generated maps retain native key types, integer widths, enum membership, model ownership and custom text contracts. Canonicality and identity checks precede DTO hydration; response output follows the same rules. Combined transport full regression passed.

- Native custom JSON value contracts passed focused runtime, fresh-generation, consumer, compiler and actual-gopls acceptance. Generated DTOs reuse typed JSONContract methods, shared scalar declarations and owned normalized schemas. Incompatible types, conflicting definitions and factory failures reject construction. Combined transport full regression passed.

- Typed path/query scalar metadata passed focused runtime, fresh-generation, consumer, compiler and actual-gopls acceptance. Native widths, UUID identities, shared decimal/temporal formats, enum membership and copied metadata use the same typed codecs as runtime execution. Custom codec descriptions retain their concrete Go value type. Combined transport full regression passed.

- JSON/query presence acceptance passed through real HTTP endpoints using generated DTOs and typed rules. Omission, nullable input, blank text, collections, zero and false preserve domain values; invalid representations, conditional triggers and source-specific errors are covered. Multipart presence remains with file transport.

- Typed signed routes and endpoints passed focused runtime/consumer/compiler/editor acceptance and bounded verification fuzzing. Temporary URLs bind exact escaped input, actual approved origin, route and method; key rotation, expiry, duplicate rejection and verification before domain decoding are covered. Their combined transport full regression passed.

- Typed cookies and signed cookies passed focused runtime/consumer/compiler/editor acceptance and bounded cookie-input fuzzing. Scalar codecs preserve named values, model IDs and enums; scoped deletion, duplicate detection, expiry, key rotation and verification before domain decoding are covered. Combined transport full regression passed.

- Approved public origins, typed proxy origin sources and canonical link generation passed focused runtime/consumer/compiler/editor acceptance and bounded URL fuzzing. HSTS now recognizes explicitly trusted public HTTPS without confusing a configured URL base or internal TLS with the incoming public scheme. Combined transport full regression passed.

- Security-header core supplies typed frame/referrer policies, explicit native-TLS HSTS, bounded custom header values and copied response defaults while preserving native writer capabilities. HTTP/TLS consumer and runtime races, two compiler rejections, two actual-gopls probes, vet, formatting, freshness and documentation checks passed. Typed CSP, request nonces, source/hash/sandbox declarations and reporting policies also passed focused runtime/consumer/compiler/editor acceptance and bounded source-parser fuzzing. Combined security-header full regression also passed.

- Trusted proxy middleware uses explicit typed peer networks and ordered header sources. Bounded IPv4/IPv6 forwarding chains stop at untrusted or unknown hops and enrich shared client attribution without rewriting native transport state. HTTP/consumer races, targeted parser fuzzing, two compiler rejections, two actual-gopls probes, vet, formatting, freshness and documentation checks passed. Trusted-proxy full regression also passed.

- Typed CORS configuration snapshots origin, method and header policies into shared middleware assembly. Bounded preflights, credential rules, cache variation, shared errors and native writer capabilities passed HTTP/consumer races, targeted header fuzzing, two compiler rejections, one actual-gopls probe, vet, formatting, freshness and documentation checks. CORS full regression also passed.

- Typed middleware declarations compose native HTTP wrappers in explicit parent/child/route order, retain matched-route metadata before decoding and preserve native writer capabilities. Duplicate IDs and constructor failures reject assembly. HTTP/consumer races, one compiler rejection, one actual-gopls probe, vet, formatting, freshness and documentation checks passed. Combined full regression also passed.

- Advisory model validation reuses typed stored fields, query scopes and parameterized existence execution. It preserves exact decimals, natural keys and explicit soft-deletion visibility; typed update exclusions do not reserve values or replace constraints. Focused runtime/consumer races, real PostgreSQL, four compiler rejections, one actual-gopls probe, vet, formatting, freshness and documentation checks passed. Combined full regression also passed.

- Typed required/prohibited rules and empty-content checks preserve omitted, null, blank, zero and false states. Field display labels retain exact wire paths and share runtime/contract metadata. Focused runtime and consumer races, four compiler rejections, two actual-gopls probes, vet, formatting, freshness and documentation checks passed. Combined full regression also passed.

- Typed validation rules retain request and field value types, exact numeric bounds, optional/null semantics and owned public metadata. DTO generation supplies field selectors from existing JSON discovery. Typed endpoints validate before handlers and export safe shared issues and truncation metadata. Focused runtime/consumer races, generator/freshness checks, ten compiler rejections and vet passed; all fourteen new compiler cases and three actual-gopls probes now pass. Full regression verification also passed.
- Additional validation rules preserve conditional budgets, native membership values, enum wire cases, collection bounds, pointer/absence states, text formats and temporal comparison precision. Timezone loading is shared with query validation. Metadata distinguishes custom wire transforms from native values. Focused races, consumer generation, eleven additional compiler rejections and four actual-gopls probes passed; combined full regression also passed.

- Typed endpoint descriptors bind generated path/query/body inputs and concrete
  response DTOs, with bounded decoding, safe field issues, schema-checked response
  preparation and owned inspection metadata. Request-context deadlines preserve
  parent constraints and active handler ownership. Focused runtime/consumer races,
  generation/freshness, six compiler-rejection cases and vet passed; canonical
  consumer/editor acceptance also passed. Full regression verification also passed.

- Native/named float path and query bindings retain concrete widths, rounding and custom text-codec precedence. Path callbacks and HTTP error classification now own panic/Goexit recovery and wait for completion. Focused race, generator, consumer, fuzz and compiler-rejection checks passed. Canonical generation, consumer and actual editor checks also passed; full regression verification also passed.
- Typed query descriptors and generated query bindings preserve required/optional/repeated field types, model identities, enum validation and text codecs. Shared bounded parsing/encoding rejects malformed input and ambiguous scalar duplicates. Focused runtime/generator/consumer checks, query fuzzing and six additional compiler-rejection cases passed; canonical generation, consumer checks and actual-workspace editor probes also passed. Full regression verification also passed with real PostgreSQL, actual gopls and current generated output.

- Typed JSON response encoding and slice/nullable descriptor composition reuse generated schemas, preserve concrete DTO owners, and return no body on failure. Shared input inspection and output limits, codec cancellation/panic handling, native omission behavior and new compiler-rejection checks passed focused acceptance. Canonical consumer/language checks and full repository regression verification also passed.

- Generated typed JSON DTO descriptors with exact field names, required/null semantics, shared enum cases, scalar formats and bounded decoding. Persistence models require explicit response DTOs. Full repository acceptance, focused runtime/generator/consumer checks, canonical generation and actual-workspace language probes passed.

- Generated typed HTTP path descriptors from handwritten structs, with shared runtime/generator grammar, automatic codec selection, enum validation and existing text-codec support. Full repository acceptance, focused generator/runtime/consumer races, fresh-checkout and stale-generation tests, and four additional compiler-rejection cases passed.

- Typed route/path descriptors, native route conflict detection, immutable scopes, named URLs, concrete model-ID/natural-key codecs and route inspection. Runtime/consumer races, compiler-rejection tests and bounded URL fuzzing passed. Ambiguous encoded slash-only segments reject before dispatch; internal path failures retain safe request/route diagnostics through the injected kernel logger.

- Streaming HTTP body limits, generated request IDs through shared attribution, and a typed built-in error catalog with safe JSON responses. Full repository verification passed; focused runtime/consumer races cover chunked uploads, early rejection, error identity, header preservation and correlation.

- Initial HTTP kernel with explicit native I/O bounds, pure handler construction, application logger injection and dependency-preserving handler draining. Runtime/consumer race checks passed; typed transport features and full milestone acceptance remain in progress.

- Typed model and domain auditing with generated field policies/history, transaction-bound observers, sensitive-value redaction, explicit migrations and bounded retention through the shared query compiler. Runtime/consumer races, all compiler-rejection and gopls checks, complete normal repository coverage and the full generator race suite passed.

- Generated `FoundryReference` and `FoundryIdentity` reuse stored primary-key codecs, preserve concrete model/key types and avoid presentation getters. Immutable model/system attribution captures provenance without copying credentials or resolving another model. Full repository acceptance and the generator race follow-up passed.
- Transactional event outbox with payload-owned message IDs, explicit migrations and generated typed storage. PostgreSQL and consumer races, fresh-checkout generation/compilation, compiler-rejection, real-gopls and full repository acceptance passed.
- Typed event topics/listeners, bounded ordered dispatch, explicit provider registration and transaction-aware after-commit publication. Payload and attribution snapshots preserve ownership across listeners and commits. Runtime and consumer races, compiler and gopls checks, and full repository acceptance passed.
- Generated `Update<Model>From` and `Delete<Model>Using` builders preserve typed source/model ownership, fixed setters, timestamps and deletion visibility. SQL-mapped updates reject ambiguous source matches and roll back. PostgreSQL, compiler-rejection, real-gopls, full repository and generator race acceptance passed.
- Generated `Insert<Model>From` builders select typed SQL values into models, preserve literal draft setters and database defaults, and expose affected counts or bounded complete-model results. Per-model observers remain an explicit `CreateEach` alternative.

- Typed `FirstOrCreate` and `UpdateOrCreate` reuse ordinary model writes, prepare only the chosen branch, lock existing models before callbacks, and validate created models against lookup scopes. Concurrent insert conflicts retain ordinary errors without hidden retries.

- Typed `CreateEach`, `UpdateEach`, `DeleteEach`, `RestoreEach` and `ForceDeleteEach` run ordinary model behavior in bounded atomic batches. Selected rows are locked and checked before callbacks; later failures roll back earlier writes and after-commit work. Relation detachment shares the same selection runtime.

- Typed relation attach/detach derives pivot keys from existing descriptors, preserves explicit draft inputs and ordinary lifecycle, validates stored links, and bounds atomic removals. Endpoint locks and database uniqueness retain explicit concurrency behavior.

- Convention-based soft deletion, typed restoration/force deletion, independent target/pivot visibility, managed deletion field notices and operation-aware lifecycle changes share the model query/write pipeline.

- Managed model timestamps recognize `CreatedAt`/`UpdatedAt`, document their behavior beside fields, and use the actual transaction owner's application clock. Typed overrides, strict explicit-value precision, hook ordering, change tracking, immutable bulk inputs and normalized upsert timestamps share the existing write runtime.

- Field mutators accept distinct concrete input and stored types. Generated drafts, before hooks and conflict setters retain the input type; stored comparisons, codecs, keys and change snapshots retain the output type. Automatic field notices document both types, and draft formatting omits captured values.

- Generated typed retrieval hooks and provider observers run after complete-model hydration and row closure. Model/result metadata carries them through aliases, CTEs, sets, locks, pagination and relations; callback-capable iteration uses bounded batches. Getters remain explicit, and write hydration, scalar reads and DTO projections skip retrieval dispatch.

- `Rows.WithObserverScope` retains callback work independently of its SQL stream, permitting callback I/O after row closure while preserving query/owner cancellation and shutdown draining. Generated retrieval adapters use this runtime boundary.

- The immutable observer registry accepts separate typed retrieval factories through existing database registration, with pool-wide name uniqueness and independent write/retrieval lookup.

- Row streams retain the actual database owner's immutable observer metadata through executor wrappers and after stream closure. Metadata inspection does not invoke hooks or extend connection ownership.

- Explicit `Access<Field>() (Result, error)` model getters retain stored fields. Generation automatically documents getters and persistence mutators beside handwritten fields and in query/draft APIs, with source-preserving publication, stale checks and recovery. Readable agent completion retains the language server's field documentation.

- Typed observer declarations, dependency construction and immutable pool binding retain model/hook types, provider order and session/transaction/savepoint ownership. Generated model-owned registration helpers dispatch normal writes by callback stage, sharing one draft, once-only mutators and captured changes. Models without local factories and writes through transactor wrappers retain their registered observers; bulk writes still skip them.

- Typed `foundation.ResolveAll[T]` collects explicit service contributions in provider/registration order through the existing constructor graph, retaining once-only construction, cycle detection, frozen resolution scopes and independent result slices.

- Explicit model hooks factories generate concrete create/update/delete callbacks, mutable access to immutable drafts, and automatic stored change capture. Normal writes lock existing rows, run callbacks in their transaction/savepoint, and defer after-commit callbacks to the outer commit; bulk operations retain separate semantics.

- Generated model change sets and field sets compare complete persisted snapshots through shared codecs, preserve model-owned drafts and field types, exclude transient/relation state, and reject mixed primary identities.

- Typed lifecycle field-change primitives preserve before/after values, assignment state, model absence and NULL. Comparisons reuse codec representations, retain interval calendar components and omit values from routine diagnostics.

- Automatic `Mutate<Field>` discovery with exact field signatures, fresh-checkout validation and generated registrations. Normal writes and explicit bulk/upsert draft inputs transform assigned values once inside their transaction, preserve omission/NULL, validate transformed values and retain caller drafts.

- Transaction-required CTEs, derived records and joins with SELECT-owned locks, complete generated projection builders, shared compiler/decoder paths and protection against conversion into unrestricted query sources.

- Transaction-required scalar, membership and EXISTS subqueries with exact nullable codecs, immutable value locks and shared computed membership traversal, binding and qualification.

- Transaction-required correlated records/values and lateral joins with explicit parent ownership, generated correlated projections, per-parent locks and nested nullable scopes. Ordinary and transaction queries share correlation binding, scope helpers, predicate derivation and lateral input construction.

- Typed per-input row-lock clauses with independent strengths and wait policies, immutable scope-owned descriptors, bounded validation and transaction-required result execution.

- Typed expression and partial-index conflict targets through `OnConflictKeys` and `TargetWhere`, with codec-validated schema constants, independent execution bindings and shared expression compilation.

- Typed conflict calculations with separate stored/proposed record scopes, exact and nullable field assignments, conditional updates and scalar/correlated subqueries through the shared AST and transaction runtime.

- Generated JSON property, array and typed map paths with nullable scalar filters, recursive payloads, snapshot selection and explicit missing/null predicates. Runtime validation and generation share JSON field/tag promotion; custom serializer signatures are discovered on fresh checkouts.

- Typed JSON model/projection fields, immutable payload snapshots, strict bounded decoding and PostgreSQL JSONB codecs. Whole-document predicates, containment and kind inspection retain payload types, model ownership, nullability and query phases through the shared AST.

- Interval model/projection codecs preserve signed months, days and elapsed time across PostgreSQL output styles. Generated interval fields supply typed summaries, predicates and drafts; query expressions add interval arithmetic, differences, components and dynamic temporal shifts. Relation grouping follows SQL interval equality without normalizing stored model values.

- Typed temporal extraction, truncation, timezone conversion, calendar/elapsed arithmetic and exact Unix-millisecond conversion through the shared expression AST. Explicit units, zones, DST resolution, nullable results and row/selected phases preserve compiler-visible contracts.

- Typed inner/left/cross lateral joins with non-executable correlated records, generated fluent report selectors, per-parent windows, preserved nullable scopes and shared query compilation/decoding.

- Typed row/selected value comparisons, computed and non-equality join conditions, explicit NULL-aware comparisons and Cartesian joins. Scalar row-subquery helpers preserve inner SELECT phases, correlation ownership and existing cardinality errors; PostgreSQL FULL JOIN planner limits remain explicit.

- Typed arithmetic and text calculations with scope-owned row/selected APIs, exact numeric conversions, nullable results and typed comparisons. Computed model orders share normal reads, pagination, chunking, locks and relation loading; computed cursor identities require declared projection fields.

- Typed numeric/temporal RANGE frames with scope-owned distance boundaries, exact/nullable values and separate calendar versus elapsed intervals. Reusable named windows share definitions and bindings while enforcing PostgreSQL inheritance, grouping and SELECT-local scope rules.

- Computed row grouping, window partition and DISTINCT ON keys, plus selected aggregate/window keys through `ProjectionKey`. Shared key matching preserves bound parameter identities, grouped subexpressions, scope ownership and existing field descriptor APIs.

- Typed row and selected conditional expressions for CASE, COALESCE and NULLIF, with explicit nullable promotion, row-predicate boundaries and shared grouping/window/correlation analysis. Standalone parameters retain codec-owned SQL types and immutable encoded values, including exact decimals, model IDs and temporal values.

- Typed aggregate `Filter` predicates for independent grouped measures, windows and relation slots, preserving field ownership, exact/nullable results, CTE/correlation composition and unfiltered cardinality checks.

- Typed `CursorFor` and `ValueCursorFor` pagination for complete projection/model, CTE, set and scalar results. Output scopes and explicit `UniqueBy` keys share generated codecs, nullable bidirectional boundaries and bounded tokens; source grouping, windows and distinct selection remain intact. Result items remain native Go slices.

- Transaction-scoped typed row locking for models, projections and selected values, with four PostgreSQL strengths, waiting/`NoWait`/`SkipLocked`, generated natural/model-key lookup and typed `Of` join scopes. Locked reads preserve complete results and eager parent loading while preventing execution through a pool or composition into unrestricted query sources.

- Typed model `Upsert`, `CreateMany` and `UpsertMany`, with composite/named conflict targets, incoming/constant/NULL assignments, stored-row conditions and explicit skipped results. One insert compiler preserves per-row omission/defaults, bounded atomic batches and complete returning results; transaction outcome errors retain the terminal's typed candidate.

- Typed `Chunk`, `ChunkByID`, `EachChunked` and `EachByID` model iteration, with preserved total windows, natural-key traversal, closed rows before callbacks and eager-load budgets per batch. `Each` now supports eager relations through bounded batches.

- Count-free simple model pagination and numbered/simple pages for typed projections, sets and selected values, with shared metadata/failure handling and preserved nested-query semantics. Simple/cursor eager loading now excludes the hidden lookahead row and releases its retained references.

- Typed window expressions with scoped partitions/orderings, ranking/distribution/navigation functions, aggregate windows, row/peer frames and exclusions; nullable results, exact codecs, CTE/correlation/recursive composition and typed projection boundaries share the query runtime.

- Typed `Distinct` and PostgreSQL `DistinctOn` read queries for complete models, projections and values; shared SELECT compilation preserves selected-row counting, scoped keys, parameter-aware ordering, CTE/set/correlation composition and ordinary typed slice results.

- Typed recursive CTEs for complete model and projection hierarchies, with UNION/UNION ALL semantics, owned working-table references, shared dependency/compiler infrastructure and PostgreSQL recursion validation.

- `SelectRecord` for complete typed models or declared reports from preserved model/alias/join/set scopes, reusing generated decoders and read-only query terminals while rejecting nullable or incomplete source scopes.

- Typed union/intersection/difference operations and their duplicate-preserving forms for complete records and single values, with output-owned fields, independent input/result windows, CTE/correlation composition and shared projection/set execution.

- Typed read-only CTE descriptors for complete model/projection records, shared dependency ordering and materialization options; joins, nested filters, model writes and relation loading use the existing compiler, codecs and result types.

- Generated `WhereHas`/`WhereDoesntHave` and parent-owned relationship existence predicates for direct and many-to-many relations, with deterministic nested aliases, target/pivot filters, reusable eager descriptors and typed model query results.

- Explicit typed correlated EXISTS, IN and scalar subqueries, nested outer scopes, nullable join scope adapters and column comparisons; the shared compiler preserves grouping rules, source isolation, parameter bounds and related-model qualification.

- Projection queries as typed derived sources, generated record field sets and nullable outer-join fields; single-value queries, typed IN/nullable-IN, EXISTS and scalar subqueries reuse the shared SELECT compiler and projection execution.

- Typed model aliases and inner/left/right/full joins, preserving source filters/windows, scoped operators, composite ON conditions and outer-row nullability; generated fluent projection builders infer complex joined scopes.

- Typed HAVING predicates and aggregate/expression ordering for projections, with count/numeric/null-aware comparison capabilities, explicit grouped-column predicates, shared SQL binding and compile-time row/group scope separation.

- Generated projection records, scope-owned selection structs and complete decoders for partial reads, grouped reports and scalar aggregates; typed nullability, explicit nullable promotion, pre-execution mapping validation and result-window counting share the common SELECT compiler.

- Generated typed relation aggregate slots with row/field/distinct counts, existence, extrema and nullable numeric summaries; exact integer/decimal arithmetic, pivot inputs, nested loading and shared budgets use the common grouped SELECT compiler.

- Generated many-to-many descriptors and loaded collections with concrete pivot models, separate typed scopes, combined database ordering, nested target/pivot loading and shared resource bounds through the common SELECT compiler.

- Numbered framework blueprints covering architecture, Rust module parity, typed persistence, model-first authentication, storage, five kernels, realtime, contracts, plugins and release gates.
- Initial Go module and documented public package boundary.
- Independent consumer import fixture, standard-library repository checks, and Make verification commands.
- Contributor and agent guidance for milestone-by-milestone framework development inside the project microVM.
- Shared application lifecycle with deterministic providers, typed constructor injection, five kernel contracts, managed critical tasks, and partial-startup/reverse-order cleanup.
- Typed layered configuration, source-only diagnostics, safe secrets, application-owned structured loggers, and controllable application clocks.
- Model-owned UUIDv7 identities, omitted/null/present value types, and immutable temporal values with explicit timezone/DST behavior.
- Public consumer lifecycle/value tests, negative compilation assertions, and targeted foundation race/fuzz coverage.
- Model/enum generation from handwritten Go declarations, complete-package overlay checking, deterministic owned outputs, stale-generation checks, and rollback for ordinary publication failures.
- Model-owned query predicates/orderings, type-specific field operators, generated mutation drafts, and validated string/integer enum serialization.
- Consumer tests for generated APIs and compile failures for wrong owners, values, IDs, operators, and nullable mutations.
- Agent completion, hover, and definition commands over standard LSP with unsaved consumer buffers, Unicode-aware positions, bounded process lifetime, protocol fixture coverage, and passing real-gopls consumer acceptance.
- Typed enum descriptors, exact serialized contract values, validated standard SQL codecs, and scalar-only operators for imported generated enums.
- Journaled generation recovery with active-process exclusion, restored deletions/creations, preserved permissions, and refusal to overwrite concurrent edits.
- Fresh-checkout declaration analysis supporting generated aliases, ignored fields, inherited enum constants, and generated import-name collisions.
- Recursive package-graph generation with checked in-memory dependencies, validation before publication, one recovery decision across packages, module/workspace input checks, and confined filesystem operations.
- Bounded TOML configuration loading through the existing typed schema, strict JSON collection keys, namespace ownership checks, value-free provenance, and consumer precedence tests.
- An explicitly invoked development-tool installation target with a separate gopls version pin; the tool adds no framework runtime dependency.
- Database runtime contracts for bounded pool acquisition, draining shutdown, parameterized execution, streaming rows, callback-scoped transactions, savepoints, and explicit commit/after-commit outcomes.
- Prepared pool construction and typed application modules, sequential connection sessions, uncertain-transaction disposal, and classifier failure isolation.
- Immutable migration registries, canonical checksums, drift inspection, a PostgreSQL session-lock/history runner, and transactional seeders with shared deterministic dependency ordering.
- Consumer migration/seeder commands with typed resources, service-free argument parsing, JSON/text reports, and confirmed progress retained on failures.
- Migration/seeder scaffolding with consumer-package compilation, explicit stable identities, exclusive file creation, shared journal recovery, and consumer-owned implementations.
- Approved pgx PostgreSQL adapter with explicit typed endpoint/TLS settings, bounded statement caching and protocol messages, SQLSTATE classification, and application-owned pools.
- Non-destructive PostgreSQL acceptance and public testing helpers covering streaming, cancellation, constraints, transaction outcomes, serialization/deadlocks, concurrent migrations, drift and seeding; a required race-enabled command also verifies the independent consumer against the database.
- Typed SQL codecs for named scalars, model IDs, nullable values, generated enums, exact decimals and temporal types, with overflow/precision checks and assignment only after successful decoding.
- Immutable exact decimal values with canonical equality, bounded arithmetic and exact string JSON; generated decimal fields/drafts and consumer compilation-failure coverage.
- Generated enum codec functions shared by standard SQL interfaces and consumer persistence, verified with valid and malformed values against PostgreSQL.
- Shared PostgreSQL model-query compiler with parameterized predicates, quoted identifiers, literal substring escaping, declaration checks and resource bounds.
- Generated complete-model hydration and typed fluent queries supporting collection/streaming reads, optional/required lookup, concrete primary keys and selected-window aggregates; compiler and real PostgreSQL consumer coverage verify their behavior.
- Generated typed create/update/delete methods, immutable draft assignments, database-default markers and automatic omitted UUID keys; a shared compiler and transaction pipeline provide scoped single-row writes with complete returning hydration.
- Composable model-write savepoints, validation before transaction acquisition, rollback on returning-row failure, and typed reconciliation candidates preserving uncertain or committed transaction outcomes.
- Numbered model pages with checked offsets and totals, plus bounded model-owned cursor pagination with generated typed getters/codecs, stable primary-key tie-breaking, nullable/mixed-direction sorting, backward navigation and query-scoped versioned tokens.
- Explicit singular/collection relation states, generated typed relation sets and ordinary Go key declarations, supporting direct belongs-to/has-one/has-many, self/natural keys and imported targets.
- Batched scoped/nested eager loading, explicit Load/LoadMissing, singular-cardinality failures and shared limits for fetched rows, nesting and attached-model expansion.
- Generalized generated field metadata to `ModelField`/`NewModelField`, shared by cursor and relation loading; this replaces the earlier cursor-specific declaration names.

- Added unverified typed `password.Hash` model/projection support, shared sensitive
  codec metadata, automatic field notices, hash-aware audit redaction and rejection
  of sensitive cursor/identity keys. Typed writes reject strings and plaintext.
- Added unverified `auth.PasswordLogin` and model-owned results: one login lookup,
  shared provider eligibility, bounded dummy verification, conditional rehash with
  no uncertain-write retries, and explicit pending/full MFA assurance. Consumer,
  failure, compiler and editor coverage is written for the milestone gate.

- Added unverified typed login lockout with atomic Redis/local authorities,
  generation/revision checks, bounded callbacks, explicit reset and typed lock
  observations. Password login composes it through `WithLockout`; shared HTTP
  errors include 429 Retry-After and protection-store failure responses. Existing
  request-rate limits remain separate. Tests are written for the milestone gate.

- Added unverified typed account-recovery flows: model/purpose-owned reset and
  verification tokens, shared hashed PostgreSQL storage, current-state binding,
  atomic model mutation/consumption and bounded pruning. Provider.CheckModel
  validates a locked model without a second lookup. Tests and consumer fixtures
  are written for the milestone gate; session/token invalidation integration,
  recovery HTTP composition and MFA remain required.

- Added unverified transactional session/token revocation and typed multi-guard
  invalidation, with exact pool ownership and scoped schema restoration. Password
  proofs now recheck a locked current model during issuance to close reset races;
  scope narrowing preserves that check. Consumer and failure/race/type/editor
  coverage is written for the milestone gate, including foundation regression.

- Consumer startup C02: typed named/default database, Redis, storage and cache assembly; nested schema-decoded tables; shared cloud credentials; bounded file/PostgreSQL caches. Acceptance is tracked in the master blueprint.
