# 08 — HTTP, validation, and responses

**Status: complete.** Typed HTTP kernel/routing, generated transport contracts,
validation, middleware, proxy/security policies, cookies, signed URLs, pagination,
model binding, uploads/downloads/streams, static/SPA, ETags and gzip/Brotli
compression are delivered. Focused races and full canonical regression passed
with required PostgreSQL, actual gopls, all 641 compiler-rejection cases and
current generation. The [completion review](00-master-architecture-and-parity.md#milestone-08-completion-review)
records evidence and the consumer boundary.

## Purpose and prerequisites

Prerequisites: [02](02-foundation-and-application-lifecycle.md), [03](03-generation-and-language-tooling.md), [05](05-typed-model-and-query-core.md), [07](07-model-lifecycle-events-and-audit.md). Preserve thin, typed HTTP boundaries while retaining standard `net/http` interoperability.

Rust references: `src/http`, `src/validation`, `src/kernel/http.rs`; `tests/http_edge_acceptance.rs`, `http_model_path_acceptance.rs`, `validate_derive_acceptance.rs`; Starter's `src/portals` usage and DTO conventions.

## Public boundaries

The HTTP package owns route registration, transport decoding/encoding, middleware integration and its kernel. Handlers call injected domain services and return explicit DTOs; models are not automatically exposed as response schemas.

Use typed endpoint descriptors binding request, response, path/query types, route ID and handler together. Representative domain handler shape:

```go
func UpdateProfile(ctx context.Context, request UpdateProfileRequest) (UserResponse, error)
```

Generated or generic adapters perform validation and transport work around that handler. Standard `http.Handler` remains supported as an explicit raw endpoint, with contract-export limitations declared. Do not require reflection-based arbitrary argument injection.

## Implementation slices

1. HTTP kernel with timeouts, maximum body size, graceful drain, request IDs, panic recovery, and injected logger.
2. Typed route/scope registration, duplicate and route-ID validation, parameter decoding, named URL generation, route inspection, and explicit access metadata.
3. JSON/query/path binding with typed model IDs, structured field errors and a stable response/error envelope. Distinguish omitted and null patch fields.
4. Generated validation from the common metadata pipeline, typed reusable rules and request-level hooks. Keep database validation advisory where database constraints remain authoritative.
5. Middleware ordering, CORS, trusted-proxy handling, security headers, automatic ETags, compression, request limits, cookies and signing. Credential-based session/CSRF integration is completed in milestone 10.
6. Multipart streaming interfaces, downloads, range/conditional handling where supported, static assets and SPA fallback. Storage-backed integration belongs to milestone 11.

Route metadata contributes directly to the normalized contract manifest as endpoints are registered. Validation metadata is derived from the same rules used at runtime; do not maintain a separate frontend validation definition.

### Validation parity and typed execution

The Rust implementation's `src/validation/rules.rs`, `validator.rs`,
`extractor.rs` and `tests/validate_derive_acceptance.rs` establish the following
validation behavior delivered by the typed rule engine and its transport adapters.
Decoding and domain validation remain separate operations.

| Rule family | Required Go behavior |
| --- | --- |
| Presence | Required, present, prohibited, nullable and conditional presence; distinguish omission, null, empty text and empty collections |
| Typed values | Text length and patterns, email/URL/IP/UUID representations, enum membership, numeric bounds and temporal comparisons; retain native value types and exact decimals |
| Related fields | Confirmation, same/different and required-if/unless/with use compiler-checked field references and export the declared wire names |
| Collections | Typed element rules, indexed field paths, distinct values, empty collection handling and bounded traversal |
| Control flow | Deterministic rule order, field-level bail, optional-value skipping and request-level validation hooks |
| Database checks | Model/field-aware existence and uniqueness checks with injected executors; advisory checks do not replace database constraints or create implicit request transactions |
| Extensions | Typed custom rule values, duplicate identifier detection, explicit dependency injection, owned metadata and panic/cancellation handling |
| Public errors | Stable rule codes, safe declared messages and field display names; one runtime description supplies contract metadata. Localized catalogs integrate in milestone 20 |
| Files | Required/optional uploads, size, content type and extension rules, cleanup on failure and cancellation; image dimension/content checks integrate with the imaging milestone |

Go generation must reuse the shared field/tag and wire-name discovery already
used by DTO contracts. It must reject invalid rule/value combinations and
unknown or incompatible related fields before replacing generated output.
Handwritten request hooks remain separate from generated files. Normal typed
validation must not stringify every field or require application code to perform
string field lookups.

Presence-sensitive rules need explicit presence information. In particular, a
plain zero-valued field cannot prove whether its JSON member was supplied.
Reuse the declared Optional/Nullable contracts and preserve any additional
validated presence metadata needed by `present`, `prohibited` and conditional
rules; do not infer presence from Go zero values. JSON, query and multipart
adapters must agree on this behavior at their respective boundaries.

Transport acceptance preserves this matrix. Rust's
`execute_steps` distinguishes `present` from a trimmed empty value; Go must
retain that behavior without stringifying native numbers, booleans or collections.
The rules preserve these states through their declared Optional/Nullable contracts:

| Input state | Present | Required | Prohibited | Strictly absent |
| --- | --- | --- | --- | --- |
| Omitted | Reject | Reject | Accept | Accept |
| Explicit null | Accept | Reject | Accept | Reject |
| Empty or whitespace-only text | Accept | Reject | Accept | Reject |
| Empty collection | Accept | Reject | Accept | Reject |
| Supplied numeric zero or false | Accept | Accept | Reject | Reject |
| Supplied nonempty value | Accept | Accept | Reject | Reject |

Conditional presence uses the same definitions. `required-with` activates when
at least one referenced field is supplied and nonempty, including supplied zero
or false. Related-field references must preserve the request and value types.
Strict `Absent` is useful but does not replace prohibited-value behavior.
Domain validity, such as a non-nil model identity or a valid calendar date, remains
an additional typed value constraint; do not treat every Go zero struct as null.

Rule callbacks execute with the request context and retain resource ownership
until return. Distinguish rejected user input from failed infrastructure or
panicking rule code; only validation rejection maps to 422. Expose bounded,
owned issue collections. A rule's internal error text and the submitted value
must not become its public message. Rules requiring a server capability must
remain identified as server-only in exported metadata instead of promising
equivalent browser enforcement.

Acceptance includes the Rust reference's renamed wire fields, conditional
presence, optional and typed collections, custom messages, bail, manual hooks,
custom rule failures and multipart cleanup, plus Go compiler-rejection cases.
These checks accompany implementation and precede closing this milestone.

## Automatic pagination at the HTTP boundary

Laravel-style pagination convenience is required. The database layer already owns typed numbered/cursor requests, query execution and page metadata; HTTP adapters must reuse those contracts. Declare endpoint pagination once so Foundry parses and validates page/size or cursor inputs, applies configured defaults and maximum sizes, and supplies a typed request to the handler. Consumers must not repeat offset calculations or query-string parsing. Support explicit parameter names for multiple paginators and reject ambiguous inputs.

Provide typed page response adapters with items, metadata and next/previous links. Mapping models into explicit response DTOs must retain pagination metadata without automatically exposing persisted fields. Link generation preserves declared filters, uses validated route/proxy configuration and escapes input; it must not trust an arbitrary incoming host. HTTP request discovery belongs here, while the same database paginator remains usable from jobs and CLI commands without a global request object. Contract generation exports the same pagination request/response shapes to OpenAPI and TypeScript.

Acceptance includes omitted/invalid/oversized page inputs, empty/beyond-last pages, typed cursor validation, custom parameter names, filter-preserving links, DTO mapping and a thin consumer handler. Core model pagination, numbered/simple projection pagination and [typed result cursor pagination](../docs/guides/result-cursor-pagination.md) are delivered. Numbered/simple [HTTP adapters](../docs/guides/http-pagination.md) now reuse those contracts and passed focused acceptance, including an independent generated consumer, explicit model-getter mapping, five compiler rejections, two real-gopls probes and a generated main executable. [Cursor HTTP adapters](../docs/guides/http-cursor-pagination.md) now retain model/projection ownership through typed DTO mapping, request validation and directional links; focused acceptance passed. Combined transport full regression passed. Simple page responses must not invent totals that their database contract does not compute.

## Typed route-model binding

[Model binding](../docs/guides/http-model-binding.md) composes the ordinary or signed endpoint with a `Resolver[Path, Model]`. `ByKey` reuses generated `Find` methods and their exact key types; `Define` supports custom keys and parent scopes through normal typed queries. Handlers receive the original request and a concrete model, while transport DTO declarations remain unchanged. Binding performs no implicit transaction or authorization and keeps no model between requests. Missing results become 404; lookup/hook failures remain errors. Cancellation and callback ownership share existing HTTP behavior.

Focused runtime/consumer races, five compiler rejections, two actual-gopls probes, isolated PostgreSQL hydration/lifecycle tests, vet, formatting, generation freshness and documentation checks passed. Combined transport full regression passed.

## Typed multipart uploads

[Typed multipart uploads](../docs/guides/http-uploads.md) now provide generated text/file/JSON field binding, typed file validation, bounded capture, and request-owned cleanup. Focused runtime, generation, consumer, compiler, editor, fuzz and allocation acceptance passed. Canonical acceptance and combined full regression also passed, including all 621 compiler cases, real PostgreSQL/gopls, current generated output and matching source fingerprints.

## Typed download responses

[Typed downloads](../docs/guides/http-downloads.md) return a concrete `Download` with declared media and deferred sources. Foundry owns native conditions/ranges, bounded transfer, shared errors and cleanup. The consumer fixture retains generated model-ID path types and delegates object selection to a small domain service. Focused runtime/consumer races, six compiler cases, three actual-gopls probes, range fuzzing, streaming benchmarks and generation/documentation checks passed. Canonical acceptance and combined full regression passed. Unseekable streams and static/SPA integration also passed the combined gate.

## Typed stream responses

[Typed streams](../docs/guides/http-streams.md) accept a deferred `io.ReadCloser` with optional exact length. They reuse file metadata, byte limits, callback ownership, transfer diagnostics and cleanup. Sources need no `Seek`; endpoint metadata declares `Seekable: false`. Ranges are ignored, HEAD reads no body, and EOF is checked before the final declared chunk is sent. Focused canonical HTTP/consumer races, six compiler cases, three new real-gopls probes, transport-cause regression, 18675 fuzz executions, bounded-copy benchmarks, vet, formatting and docs passed. Combined full regression, including all three generation freshness targets, also passed.

## Static assets and SPA routing

[Static assets](../docs/guides/http-assets.md) use explicit local or embedded sources, framework-managed lifecycle, typed mounts and URLs, declared media, native file conditions/ranges and bounded transfer. SPA fallbacks only handle unmatched navigation requests and preserve existing API and method errors. Multiple prefixes, exclusions, path confinement and source ownership have focused runtime/consumer race coverage. Five compiler-rejection cases, three actual-gopls probes, fuzzing, resource benchmarks, vet, formatting, all three generation-freshness targets and documentation checks passed. Canonical full regression also passed with required PostgreSQL, actual gopls and all 638 compiler-rejection cases.

## Automatic response validators

[Automatic ETags](../docs/guides/http-etags.md) now use typed configuration and
ordinary middleware assembly around native handlers and typed endpoints. Capture
is bounded by bytes and concurrent responses. Native conditions support weak,
list and wildcard validators; overflow, flush and native full-duplex operation
preserve streaming and complete prefixes. Source errors, cancellation and
incomplete declared bodies cannot become successful validated responses.

Focused acceptance passed HTTP and independent-consumer races, typed getter DTO
mapping with stored fields unchanged, native file/stream/static/SPA composition,
writer ownership, compiler rejection, two actual-gopls probes, fuzzing, resource
benchmarks, vet, formatting, all three generation-freshness targets and docs.
Canonical regression also passed with required PostgreSQL, actual gopls, all
639 compiler-rejection cases and current generation. Subsequent compression
integration and the 641-case full regression also passed. The original boundedness and protocol scope is preserved.

## Response compression

[Compression](../docs/guides/http-compression.md) adds typed gzip/Brotli options,
bounded prefix buffering and active-encoder limits. It composes with generated
DTO endpoints, typed downloads/streams, static/SPA responses and automatic ETags.
Native controls retain pending output, source failures abort incomplete streams,
and conditional metadata describes the selected representation. Domain services
need no encoding or writer plumbing.

Focused HTTP and independent-consumer races, two compiler-rejection cases,
two actual-gopls probes, negotiation fuzzing, allocation samples, vet, formatting,
all three freshness targets and documentation checks passed. Canonical full
regression subsequently passed and completed milestone acceptance.

## Failure behavior

Map malformed transport input, validation failures, authentication/authorization failures, missing resources, conflicts, and internal failures to stable typed error codes and documented HTTP statuses. Redact internal causes from responses while preserving them in logs. Preserve Rust’s custom HTTP failure capability through typed application error declarations, sharing code/status/public-message metadata with contract export. Arbitrary internal error text must not become a public message; localized message-key support belongs to milestone 20. Explicitly reject ambiguous or duplicate parameter sources; do not guess which supplied value wins.

Validate request size before unbounded buffering. Multipart temporary resources are owned by the request and cleaned up on decoding/validation failure or cancellation. Business transaction boundaries remain in domain operations, not implicit request middleware.

JSON decoding reuses the shared wire parser and field/tag interpretation used by runtime values and generation. Give HTTP its explicit byte/complexity limits and transport policy while preserving the existing database JSON snapshot defaults. Database `value.JSON` currently owns canonical snapshot and PostgreSQL representation constraints; those storage choices must not silently determine every HTTP DTO contract. Reuse `value.Optional` and `value.Nullable` for omitted, null and present patch fields. Generated DTO descriptors and their runtime adapters must agree on required fields, case-sensitive names, unknown/duplicate keys, custom codecs and public error details.

## Acceptance

Test typed binding, wrong-model IDs, optional fields, DTO leakage prevention, route conflicts, path escaping, malformed JSON, unknown-field policy, validation field paths, status/metadata agreement, panic recovery, cancellation, body limits, proxy spoofing, CORS, cookies, signed URL expiry, uploads/downloads and graceful shutdown. The external fixture must demonstrate a thin handler calling an injected service through real HTTP test transport. Apply the [common gate](README.md#common-completion-gate).

## Completion checklist and later integrations

- [x] Kernel ownership, deadlines, request/body limits, routing and safe errors verified.
- [x] Generated path/query/JSON/multipart contracts and typed validation verified.
- [x] Pagination and model binding preserve model ownership and explicit DTO/getter mapping.
- [x] Middleware, proxy/security policy, cookies/signing and all file transports verified.
- [x] Static/SPA, automatic ETags and bounded gzip/Brotli composition verified.
- [x] Independent consumer examples, compiler rejection and actual gopls lookup reviewed.
- [x] Full repository verification, current generation and documentation passed.

The thin consumer registers typed declarations and injects concrete services;
Foundry owns decoding, validation, transport metadata, response preparation and
resource cleanup. Native handlers retain an explicit raw transport boundary.
Business transactions and DTO field choices stay with the domain code.

Session/CSRF and credential authorization integrate in milestone 10; provider
storage in 11; realtime protocols in 14–15; image rules in 18; localization in
20; OpenAPI/TypeScript export in 21; final production hardening and release in 24.
The whole-framework verification and audit remain required after implementation.
