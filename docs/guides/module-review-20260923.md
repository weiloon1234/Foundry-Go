# Framework module review — 2026-09-23

This review follows the current Foundry-Go source and public consumer contracts. Its objective is Laravel-inspired convenience with concrete Go model, actor, payload and configuration types, IDE completion, explicit ownership and shared implementation owners.

## Current status

The review is complete for all 65 current module families, including generation, CLI, testing and release tooling. All 18 findings are fixed. The complete native gate, required PostgreSQL/Redis integration, affected races, consumer/compiler/editor/TypeScript acceptance, deterministic generation and nine fuzz targets passed. The post-verification audit found and fixed additional outbox/scheduler lifecycle paths, followed by focused races and another complete native gate. Fresh private packages, build/editor measurements and dependency/security review also passed. The [acceptance record](../evidence/module-review-20260923.json) retains source fingerprints, coverage, commands, results and limitations.

## Findings

### R01 — (root), foundation

Public introduction and NewBuilder guidance still direct ordinary applications to manual providers and omit the delivered integrated typed API workflow.

Lead README/package docs with configured application.New and typed API guide; retain direct foundation assembly as the explicit provider path. Remove stale pre-acceptance notices from accepted milestone guides and link to the authoritative master status.

### R02 — infrastructure

All nine named-service default selectors return indistinguishable missing/invalid diagnostics, leaving the consumer without the setting to correct.

Use generated typed configuration keys in the shared selection validator and identify the relative setting path without formatting configured values.

### R03 — observability, cli, internal

OutcomeFor repeatedly calls errors.Is, whose standard unwrap traversal is unbounded. A cyclic Unwrap error can strand deferred span completion and recorder shutdown even though Isolated owns the callback. CLI Status has the same unbounded Is/As traversal.

One shared private errorgraph traversal bounds outcome and CLI status classification; both preserve their own precedence, shallow custom matching and existing callback ownership.

### R04 — outbox

Publication classifies arbitrary returned errors with unbounded errors.Is while owning its transaction. Cycles strand the attempt; panicking/Goexit matching can repeatedly roll back its retry accounting.

Reuse shared errorgraph bounds with owned isolation, classify permanent reasons once, preserve unclassifiable failures for bounded retries and add actual committed-row/retry coverage. Post-verification audit extends bounded isolated classification through Publisher.Run cancellation/observer errors; PostgreSQL regression proves committed state, no duplicate delivery and reusable run ownership after cycles/panic/Goexit.

### R05 — database, internal, cli

Database classifier.inspect uses errors.As over callback-returned errors before adapter classification. A cyclic graph can strand transaction completion and its pool owner after rollback.

Use shared bounded traversal with shallow typed As matching before adapter classification; retain safe QueryFailed metadata and the transaction-owned outcome. Reuse shallow matching in CLI.

### R06 — pubsub

Canceled Receive uses unbounded errors.Is on adapter errors while retaining its reader slot; a cyclic error prevents subscription cleanup.

Use shared bounded traversal within existing isolated ownership; preserve wrapped cancellation, close unclassifiable failed streams and retain their cause.

### R07 — typescript

Schema and manifest accept MapKind without key constraints, but the TypeScript renderer dereferences nil Key and generated runtime dereferences an absent key. Valid explicit maps cannot be exported or used.

Honor absent key metadata as arbitrary string names in renderer/runtime while enforcing value schema, nullability and bounds. Cover native/serialized rendering and actual strict consumer compilation/runtime.

### R08 — schedule, internal, pubsub

Scheduler completion performs unbounded errors.Is on handler/failure-hook errors before returning its execution slot or overlap lease. Cyclic graphs can strand both despite callback isolation.

Add a shared bounded Is search and use it for scheduler and pubsub single-target classification. Cyclic scheduler failures retain handler/hook failure classification and release owned work. Post-verification audit also bounds leadership lease-error searches and isolates overlap cleanup classification; real memory-lease regressions prove terminal coordination results, retained cleanup diagnostics and reusable scheduler capacity after cycles/panic/Goexit.

### R09 — jobs, internal

Worker preparation, outcome and backend stop classification use unbounded errors.As/Is over extension errors while retaining heartbeat/execution ownership. Cyclic errors prevent retries or draining shutdown.

Add shared bounded typed matching and reuse bounded Is in worker searches, preserving search precedence, retry/terminal semantics and isolated callback ownership. Cover admission/middleware/handler retries, permanent cyclic causes and reserve/start/renew/finish failures.

### R10 — email

Custom driver or attachment error graphs can cycle during classification, retaining active sends and preventing shutdown despite returned callbacks.

Use the shared bounded walker; incomplete driver graphs remain Ambiguous and incomplete attachment graphs fail preparation. Preserve category priority and release capacity without sending or automatically retrying uncertain submissions.

### R11 — notifications

Provider lookup errors are returned unchanged and inspected with unbounded errors.Is, allowing a cyclic error to retain notification-manager ownership.

Bound inspection with the shared helper inside the existing isolated callback; unmatched failures stay retryable without rendering or delivery. Add actual PostgreSQL failure/recovery and shutdown regression.

### R12 — attachments

Storage causes can cycle while cleanup checks for absence/precondition failure, preventing retry journaling and manager release. Transaction outcome inspection has the same unbounded graph issue.

Use bounded shared inspection; unmatched cleanup errors remain pending and unclassifiable transaction outcomes stay unknown. PostgreSQL regression preserves published replacement, records failed cleanup, then reconciles successfully.

### R13 — storage

Unbounded error traversal in disk outcomes, local/S3 upload failure classification and HTTP download mapping can retain operation ownership. S3 classification panic/Goexit can bypass multipart abort.

Bound shared traversal and inspect disk outcomes once, preserve reached outcomes/cleanup references, and isolate S3 classification so source errors still reach multipart cleanup. Add local preservation/staging cleanup, real SDK peer abort/recovery, and typed HTTP download/stream regressions.

### R14 — websocket

WebSocket protocol classification, room-codec panic inspection and cluster-denial inspection used unbounded errors.Is on application/adapter error graphs. A returning but cyclic Unwrap can retain connection or cluster-startup ownership indefinitely.

Reuse bounded errorgraph inspection inside existing callback isolation. Preserve protocol-code precedence with one walk, return operation_failed for incomplete handler classification, retain malformed room replies, and treat unmatched cluster failures as terminal for active hubs. Add real-socket handler/policy/join/leave/room recovery, wire-priority and failed-cluster-startup ownership regressions; document bounds.

### R15 — http, internal

HTTP response and guarded-authentication classification and retry lookups use unbounded errors.As/Is on arbitrary returned errors. Cycles retain request ownership despite callback isolation. Idempotency mapping also repeatedly searches unknown causes.

Add shared value-bearing bounded As with completion status, reuse it in Has and HTTP response/retry searches, bound authentication mapping in one traversal and idempotency searches. Preserve typed/explicit status priority, public secrecy, known retry metadata and actual scope completion. Add route preparation/authorization/handler, guard lookup/recovery, priority and retry regressions. Bound cursor handler and custom filesystem/SPA searches; asset classification owns panic/Goexit. Actual filesystem bodies, asset leases and subsequent healthy requests are covered.

### R16 — http

Compression and ETag middleware log raw errors returned by custom I/O. slog formatting can expose arbitrary private error text or invoke nonreturning/panicking/Goexit Error methods before middleware capacity is released.

Wrap transfer failures with the existing safe framework fault at both logging call sites. Keep the original I/O cause for the handler and retain abort semantics. Add actual reader/writer failures with text/panic/Goexit formatters, injected logs and next-request capacity recovery for both middlewares.

### R17 — auth

Password lockout, recovery request issuance and MFA verification inspect arbitrary callback/adapter error graphs with unbounded standard-library searches while authentication or transaction capacity remains owned.

Reuse bounded Is/As/Has, retain conservative unknown recovery outcomes, preserve failed-password counting and rejection-observer semantics, and add cyclic failure/capacity/recovery-state regression sources.

### R18 — database

Cursor codecs and custom Transactor outcome errors were inspected without graph bounds; model write inspection also lacked panic/Goexit isolation after the external wrapper returned.

Bound cursor Invalid classification inside its existing owner. Isolate bounded model write outcome inspection; retain a typed reconciliation candidate on incomplete/abnormal inspection without claiming commit, while preserving normal known-outcome behavior. Add codec and custom-transactor regressions and consumer guidance.

## Coverage

Each family was reviewed against its actual implementation and public consumer contracts. Verification below covers the final changes.

| Family | Source review conclusion |
| --- | --- |
| `(root)` | Root delegates directly to foundation; improve configured bootstrap and typed API discovery. Documentation checks passed. |
| `application` | Configured bootstrap, typed constructor services, guards, route/kernel/feature declarations and scoped persistence compose existing subsystems without request-context service lookup. No additional change identified. |
| `attachments` | Typed model/key collections declare disk, cardinality, locale and image policies once. Conditional storage, durable intents, owner-locked publication, explicit uncertainty/retention, exact reorder, bounded bulk reads and scoped lifecycle cleanup preserve ownership. R12 bounds error inspection while retaining published and pending states; native regression verification passed. |
| `attribution` | Immutable identity/request provenance is separate from authentication authority; model capture, validation and context cancellation remain explicit. No change identified. |
| `audit` | Concrete model/key/payload audit history reuses generated stored changes and existing transactions. Redaction happens before sensitive codecs, persists explicit state and never fabricates partial DTOs; schema scopes and retention remain explicit. No additional change identified. |
| `auth` | Reviewed all authentication themes: explicit provider/guard/policy/permission registration, typed current models and scope restrictions, password/KDF/rehash/issuance checks, session and token persistence/rotation/refresh replay, recovery purpose and revision binding, transactional credential invalidation, MFA enrollment/completion/management/retirement and factor lockout. Consumer mappings keep generated model keys and concrete subjects; returned error/cancellation paths withhold authority. R17 closes three arbitrary error-inspection ownership gaps; native integration/race verification passed. Existing PostgreSQL test sources check committed completion failure, replay, fresh model state and rotation. No additional API redesign is needed. |
| `cache` | Reviewed concrete declaration ownership/typed key-value codecs, TTL/miss/snapshot semantics, bounded local and distributed Remember ownership, tags/namespace generations and stale publication, atomic entries/counters and memory budgets, file/PostgreSQL locking and persistent expiry. Consumer and shared adapter regression source supports the contracts. No new runtime defect confirmed; stale guide delivery claims corrected under R01. Native verification passed. |
| `cli` | Typed command arguments, parse/help-before-start, single-use execution and owned callback reporting remain explicit. R03 bounds shared error traversal; native regression verification passed. |
| `clock` | Explicit injected application time; system clock is UTC and lifecycle deadlines remain real. No change identified. |
| `cloud` | Shared static/chain/rotating credential boundary keeps SDK details private and owns transport startup/close; canceled retrieval and private formatting have consumer coverage. No change identified. |
| `cmd` | Development commands reuse the typed CLI registry, validate flags before execution, expose explicit create/generate/check/recover operations, and preserve output failures. Manifest reads are bounded; generated publication stays owned by existing packages. No additional change identified; private generator/doctor/agent implementations remain in the internal family review. |
| `collection` | Generic transformations preserve element/key types, documented duplicate semantics, input order and separate result containers. No change identified. |
| `config` | Typed keys share file/environment/override decoding, validated enum/width contracts and ownership copies. TOML/named tables reject ambiguous keys and keep value-free diagnostics. No additional change identified. |
| `contract` | Typed JSON descriptors share immutable graph compilation for decoding, response encoding and metadata. Generics preserve instantiated source identity; closed unions snapshot typed payloads; PATCH states, bounded paths, native numeric widths and typed map keys survive export. Manifest validates actual registered routes/status/errors, forms, idempotency, realtime and feature references; metadata cannot install runtime native codecs. Its valid unconstrained-map contract exposed R07 in the TypeScript adapter. |
| `countries` | Ordinary generated model/natural keys, explicit reference seeding and upsert-owned reference columns preserve consumer Status/ConversionRate/IsDefault. Bounded query helpers reuse extensions.Store ownership. No additional change identified. |
| `database` | Reviewed database runtime/pool/session/transaction/savepoint and observer ownership, named/default/read routing, codec/nullability/owned values, typed model and projection AST/SELECT compiler, scoped unique lookup, cursor/numbered/batched reads, typed relation loading and writes, mutation inputs/hooks/conventions, explicit set/source writes, advanced scope/CTE/set/row-lock boundaries, PostgreSQL adapter and explicit migration/history/seeder/command contracts. Current consumer source demonstrates concrete keys/values, complete hydration and bounded eager queries. R05 bounds raw callback classification; R18 bounds custom transactor/cursor inspection and preserves inconclusive typed reconciliation candidates. Source review is complete; integration and regression verification passed. |
| `datatable` | Typed source/row/value ownership connects generated DTO columns, allowlisted scalar filters, server authorization/scope, stable snapshot pagination, bounded literal CSV/XLSX artifacts, deferred HTTP and freshly authorized queued exports. Existing regression sources cover malformed filters, tenant isolation, cleanup, callback failure and retained ownership. No runtime change confirmed; replaced historical future-milestone pointers with delivered consumer guide links. Native verification passed. |
| `decimal` | Immutable comparable canonical decimals preserve exact arithmetic/JSON strings with bounded digits and no implicit rounding policy. No change identified. |
| `diagnostics` | Operational routes require a concrete authenticated actor and expose bounded lifecycle/readiness/metrics metadata, without implicit listeners or secret values. No change identified. |
| `email` | Typed templates and job DTOs, immutable rendered messages, named/default mailers, pinned storage attachments, single-attempt transports and explicit acceptance/retry semantics compose existing services. R10 bounds driver/attachment error inspection without changing known acceptance; native regression verification passed. |
| `encryption` | Immutable typed keyring, purpose/owner-bound authenticated ciphertext, explicit key retention/rotation and shared per-key nonce budget preserve ownership and redaction. No additional change identified. |
| `enum` | Typed case descriptors snapshot declarations; wire definitions preserve wide integer values and shared localization labels. No change identified. |
| `events` | Typed topic/payload/version declarations, owned synchronous listener lifetimes and stable outbox delivery identities retain type safety and explicit retry semantics. Snapshot isolation and configured construction avoid globals. No additional change identified. |
| `extensions` | Shared typed owner identity validates exact declarations, active/retained visibility and natural keys; the bounded store borrows its pool and restores schema through joined savepoints. No additional change identified. |
| `fault` | Typed classification and unwrap causes remain available while routine formatting omits internal cause text. No change identified. |
| `foundation` | Typed service graph, scoped contributions, dependency validation, managed work and reverse cleanup remain explicit and application-local. R01 updates entry-point guidance. |
| `health` | Readiness keeps declared typed probe IDs, bounded admission and timeouts while retaining callbacks until actual exit; lifecycle drains dependencies in the correct order. No change identified. |
| `http` | Reviewed typed path/query/form/JSON/multipart descriptors and shared field/schema owners; omission/null semantics, preparation/prohibitions/validation/authorization order; explicit status/application errors and metadata snapshots; concrete guard actors and nested model ownership; idempotency preparation/replay; typed pagination DTOs/cursors; cookies, credential disclosure and CSRF; signed/public URLs and proxy trust; native routing/admission/shutdown; file/assets/SPA ownership and transfer bounds; security/rate/CORS/compression/ETag middleware. Existing public consumers keep domain operations thin and typed. R15 bounds extensible failure inspection and R16 protects I/O diagnostics. Immutable metadata owners justify existing no-error snapshot paths; system/framework-only error searches were intentionally retained. The final native verification gate passed. |
| `httpclient` | Generated concrete DTO JSON, immutable request snapshots, base-prefix confinement, explicit replayable mutation retries, scoped stream ownership and bounded no-progress cleanup. Named defaults alias their existing client. Consumer handlers retain response types. No change identified. |
| `i18n` | Generated message argument types reuse the JSON contract; exact decimal plural inputs, immutable locale/catalog snapshots, bounded strict file loading and explicit request locale remain consistent. No additional change identified. |
| `idempotency` | Typed input/result codecs, primary transaction claims, caller/operation/schema fingerprints, bounded contention, explicit pruning and unknown-commit reconciliation preserve local atomicity. No additional change identified in this family; shared database error-classification follow-up remains separately tracked. |
| `imaging` | Immutable typed plans preflight input, transformed and intermediate dimensions/workspace before decoding; explicit format/orientation/frame policies, independent output bytes and owned cancellation integrate with model attachment collections. No additional change identified. |
| `infrastructure` | Named defaults alias concrete resources and provider ordering retains ownership; settings, backend references, schema scopes and migration targets remain explicit. R02 improves default-selection diagnostics. |
| `inspection` | Inspection reuses feature-owned snapshots and configuration provenance; pure collection/CLI parsing avoid service construction and typed policy metadata remains visible. No change identified. |
| `internal` | Reviewed source-owned model/query/lifecycle/configuration/DTO/union generation, complete graph checking and atomic guarded publication/recovery; typed signatures retain ownership, values and separate mutation inputs. Runtime/generation JSON shape and config rules reuse shared owners. Reviewed real crash-recovery and editor-session tests, bounded unsaved LSP overlays, owned process deadlines, offline doctor, named/default identity and stream/upload ownership. Other private store/adaptor helpers were reviewed with their public subsystem owners. Existing changes R03/R05/R15 provide shared bounded error inspection; no additional internal defect confirmed. The consolidated native gate and affected races passed. |
| `jobs` | Concrete payload capture, stable typed receipts, named/default dispatch, retry/lease ownership, bounded memory/workflow transitions and durable outbox reuse preserve the public domain-only API. R09 fixes bounded worker error classification; implementation and regression sources await the batched gate. |
| `keyspace` | Shared bounded namespace grammar and codecs retain concrete key types including full-width integers and model IDs; consumers own callback/size boundaries. No change identified. |
| `lease` | Typed resource keys, conditional owner operations, finite TTL after drift/elapsed time, explicit uncertainty, retained caller context and owned heartbeats. Proof checks current backend ownership and does not claim durable fencing. Capacity and shutdown preserve actual callback lifetime. No change identified. |
| `logging` | Application-owned slog sinks/channels snapshot stack declarations, alias defaults, deduplicate leaves and roll back failed starts; correlation remains per-call and omits vendor state. No change identified. |
| `maintenance` | Application-owned admission gate distinguishes reversible pause from terminal drain, wakes waiters and preserves existing work ownership. No change identified. |
| `metadata` | Owner/key/value types survive persistence and batched reads; snapshot codecs, version conflicts, explicit dynamic export and retained-owner orphan checks remain consistent. No additional change identified. |
| `model` | Comparable model-owned UUIDs and concrete stored-key references preserve ownership and exact persistence encoding; identity restoration checks model/codec and snapshots mutable keys. No change identified. |
| `notifications` | Concrete recipient/model/payload bindings reuse generated contracts, guard-scoped inboxes and persisted channel states. Fresh eligibility precedes each attempt; prepared snapshots freeze content, claims prevent uncertain re-send, and transaction/outbox dispatch shares existing owners. R11 bounds recipient error lookup; native regression verification passed. |
| `observability` | Typed operation names/results, bounded metrics/recent/export queues and explicit reporter/span ownership match the framework contract. R03 fixes unbounded cyclic error classification. |
| `openapi` | Render derives registered status, errors, paths, authentication and value shapes from the validated manifest. Exact integer bounds, required/null distinctions, tagged variants, form repetition, HEAD and file replies retain their wire contracts; collisions fail explicitly. No independent schema source or handler execution. No change identified. |
| `outbox` | Payload-owned IDs, explicit producer transactions and bounded fair route publication preserve durable at-least-once semantics. R04 fixes unsafe publication failure inspection; native regression verification passed. |
| `plugin` | Explicit linked plugins reuse foundation dependency/lifecycle ownership, typed namespaced configuration and migration history. Typed scaffold inputs and immutable assets reuse the guarded publisher. No additional change identified. |
| `pubsub` | Typed key/payload declarations, independently owned snapshots, confirmed readiness, bounded buffers and visible overflow/disconnect. Named defaults alias existing brokers. R06 bounds custom stream error traversal during cancellation so subscriptions can drain; regression and documentation sources ready. Native verification passed. |
| `randomtoken` | Bounded cryptographic entropy, unbiased alphanumeric sampling and explicitly revealed secret strings fit the public contract. No change identified. |
| `ratelimit` | Typed resource declarations retain exact identity and frozen policies; owned Allow/TakeWith validate cost and adapter decisions. Fixed windows use authority time, denied attempts do not consume quota, memory rejects backward time and never evicts live quota. Consumer quota is shared by typed model ID. No change identified. |
| `redis` | Reviewed all Redis Go implementation and server scripts: explicit standalone/TLS and named/default lifecycle, operation/subscription bounds, exact cache counters/tag snapshots/fill proofs, atomic rate/lockout decisions, typed hash/set declarations and canonical values, immutable raw requests/typed pipeline receipts, queue/workflow ownership and cluster presence/replay. Checked consumer use and real-service/fault regression source. No new runtime defect confirmed; stale Redis guide parity status corrected under R01. Native verification passed. |
| `sanitize` | Immutable allowlist policy, bounded input/output, passive HTML and cancellation with no partial result fit the explicit output-context boundary. No additional change identified. |
| `schedule` | Explicit calendar zones/DST, anchored elapsed intervals, stable typed occurrences, bounded catch-up and rotating admissions preserve domain-only schedule handlers. Lease ownership survives cancellation until callbacks exit; job targets retain payload/execution identity. R08 fixes cyclic domain-error inspection before capacity/overlap release. Native verification passed. |
| `secret` | Explicit reveal and safe ordinary formatting/text/JSON/logging boundaries are consistent. No change identified. |
| `settings` | Typed values and missing-only validated defaults are separate from presentation and explicit public listing. Ensure/upsert preserve user presentation and concrete integer/value semantics. No additional change identified. |
| `storage` | Reviewed all storage implementation families: exact typed keys/options/outcomes, named/default ownership, streaming/ranged/pinned reads, conditional copy/move identity, local managed publication/cleanup/locks, S3/R2 capability differences and multipart recovery, signed/public URLs and deferred typed HTTP bridges. R13 fixes confirmed traversal and multipart-cleanup gaps; no new API/driver is needed. Source/regression/consumer review complete; native verification passed. |
| `temporal` | Separate instant/calendar/local/interval values retain explicit zones, DST gap/overlap failures, exact interval parsing and injected clock ownership. No change identified. |
| `testkit` | Reviewed production application cleanup, independent namespaces/clocks, real HTTP/WebSocket clients, concrete guard/DTO/factory/event/job contracts, explicit bounded fakes and retained PostgreSQL schema/application migration helpers. Consumer tests use actual commits/rollback/outbox and named/default/read pools. No runtime change confirmed; corrected stale T07 acceptance statement. Native verification passed. |
| `tools` | Reviewed native formatting, consolidated cached package gates, deterministic private release archives/manifest and replacement-free consumers, bounded process-group measurement ownership, source hash verification, isolated compiler/editor profiles and explicit scanner semantics. Existing tests cover private-file/link exclusion, deterministic archives, source preservation, manifest tampering and failed measurements. Dedicated cold caches are intentional benchmark conditions; ordinary fixture cache ownership is checked separately under internal tooling. No tooling change confirmed; native regression verification passed. |
| `tracing` | Immutable typed trace/span identities, explicit context propagation, bounded parsing and local sampling preserve cancellation without global providers. No change identified. |
| `translations` | Typed owner fields share locale identity; one locale snapshot and bounded batched reads avoid lazy queries. Atomic assignment validation, present empty text, deterministic fallbacks and retained-owner cleanup remain explicit. No additional change identified. |
| `typescript` | Manifest is the export SSOT; concrete model brands, wide integers, generic payloads and discriminated unions survive client typing. Runtime strict JSON, explicit injected transports, owned response cleanup, request preparation deferral and bounded realtime state retain server contracts. R07 fixes valid maps without key metadata in renderer/runtime; public consumer source covers typed values, nulls, unusual names, rejection, strict compilation and runtime. Native verification passed. |
| `validation` | Generated fields retain request/value ownership; Optional/Nullable rules preserve omission, null and supplied zero/false. Conditions share work budgets, original-input prohibitions survive preparation, lookups remain explicit advisory queries, exact numeric/temporal values are not coerced. Serialized rule metadata shares runtime declaration validation; unsupported browser checks remain incomplete. No change identified. |
| `value` | Optional/Nullable keep omitted/null/value distinct; typed JSON snapshots own values and share native field/key rules with bounded transport encoding. No additional change identified. |
| `websocket` | Reviewed all WebSocket implementation: nominal channel/event/room/subject contracts, DTO metadata, exact origin/protocol admission, per-operation and background fresh authorization, pending/live presence/replay routing and quotas, cluster leases/gap termination, explicit publishers, typed diagnostics/revocation and drain ownership. R14 fixes actual unbounded custom-error traversal; consumer and real-socket/shared Redis regression source reviewed. Native compilation/tests passed for the complete batch. |

## Verification

- Final corrected-source `make verify`: 612.6 seconds, including formatting, vet, all framework/plugin/consumer packages, compiler rejection, real gopls, actual TypeScript clients, generated-output freshness, documentation and release-tool checks.
- Affected framework races: 54 packages; ten consumer race packages; post-audit publisher/scheduler/lease races and four affected consumer packages passed. PostgreSQL and Redis were required.
- Nine changed-boundary fuzz targets passed. Failed first-round assertions were corrected to inspect wrapped errors without weakening persistence, ownership, retry or capacity checks.
- Three independent package profiles reproduced generated output, built without workspace replacements and passed typed editor probes. Configured startup, selection and HTTP measurements use the existing production fixture.
- Dependency review retained every selected module and license/notice obligation. Known module-only advisories are documented in the acceptance record; no affected runtime package/function was reported.

Measurement caches created for this review were removed after evidence collection; archives, reports and logs remain. Existing live AWS/R2 certification is retained only for unchanged protocol paths, with new local SDK/TLS cleanup regressions. This review does not claim fresh provider-account certification or production throughput measurements. Error graph bounds cannot force an arbitrary non-returning custom method to exit; callback ownership remains explicit. No publication or database reset was performed.
