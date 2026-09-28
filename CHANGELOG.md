# Changelog

## Unreleased

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
