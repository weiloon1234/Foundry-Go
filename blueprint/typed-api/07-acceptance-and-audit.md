# T07 — Integrated acceptance and final re-audit

Prerequisites: T01–T06 accepted individually. Status belongs to the
[master](../00-master-architecture-and-parity.md#typed-api-delivery).

## One complete consumer proof

Extend the independent consumer fixture with a small team/project workflow. It is
a framework acceptance scenario, not a starter product. The example must use only
public framework imports and ordinary typed Go domain code. Cover:

1. Configured startup with a default and named PostgreSQL connection, explicit
   migrations and the test scope from T05. Constructor-injected services remain
   usable after assembly seals; no global/context service lookup is required.
2. An authenticated nested route using parent/project selectors and a scoped slug,
   with typed request and resource authorization, including wrong-parent rejection.
3. A PATCH DTO reusing `Optional[Nullable[T]]`. Send omitted, null, zero/empty and
   replacement values from a generated client. Validate, map explicitly to existing
   draft setters/clearers and assert persisted state through HTTP's actual pool.
4. Generic DTO envelopes, typed pagination and a tagged action outcome. Handler
   return type, encoded response/status, metadata, OpenAPI and generated TS agree.
5. Equivalent applicable rules over JSON/query/multipart/form input, with safe
   source-specific decoding and rule errors. Form literal `null` stays text.
6. A duplicate-safe submission through T06: concurrent requests create one business
   record and existing outbox row. A new app process replays the stored response.
7. Existing publisher delivery to a job/event consumer that can safely see the same
   message twice. Use a unique receipt keyed by the stable existing delivery ID in
   the same transaction as the example's receiving database effect. An external
   provider effect requires its own supported idempotency contract; do not claim
   that a local receipt makes arbitrary external I/O exactly once.

Typed PATCH support and transactional outbox are regression/integration obligations,
not new duplicate subsystems. If the integrated proof exposes an actual defect,
fix it in the existing owner and add the focused regression.

## Acceptance matrix

| Area | Required evidence |
| --- | --- |
| Go consumer experience | Public compiled fixture, concise ordinary startup/handlers, typed generic/union/binding/hook APIs |
| Type guarantees | Consolidated compiler-negative cases and real gopls completion/hover for all new public boundaries |
| Transport agreement | Actual HTTP plus generated strict TS, OpenAPI/manifest snapshots and declared error/status checks |
| Presence | Omitted/null/value preserved through client, decode, preparation, validation, draft and persisted result |
| Persistence isolation | Concurrent full apps, multiple pooled connections, reconnect, named/default routing and real commits |
| Reliable effects | Atomic claim/business/result/outbox, duplicate delivery receipts, rollback and process-crash recovery |
| Limits and ownership | Bounded parser/queue/wait/response work, cancellation, callbacks, partial startup and shutdown cleanup |
| Compatibility | Old endpoints/custom codecs/enum/pagination behavior and existing consumer/plugin fixtures stay green |
| Tooling and generation | Deterministic generation/recovery, accurate source identities and bounded schema graph expansion |
| Cost | Repeated native runtime/allocation and cold/incremental build/generation/editor measurements |
| Audit | Complete changed-code review, recorded findings, batched fixes and final-source verification |

## Verification and improvement sequence

Finish the complete source/test/docs batch before compiling/testing. Reuse the
native acceptance tooling, compiler batching, gopls scenario reuse and controlled
environment from earlier milestones. Run `make verify`, required PostgreSQL/Redis
integration where used, relevant races, new parser/protocol fuzzing, strict TS and
independent packaged-consumer checks. Collect failures, batch fixes and repeat the
affected checks until the final source passes the required gate.

Then audit all implementation changed for T01–T06 and their integrations one full
round: generator/runtime/exporter agreement, type erasure, field/source ownership,
hidden mutable state, unnecessary wrappers, decoder limits, authorization ordering,
pool/schema routing, idempotency scope, commit ambiguity, replay leakage, outbox
identity, versioning, resource lifetime and consumer boilerplate. Simplify at each
logical pause by reusing existing owners. Fix/improve confirmed findings, add
meaningful regressions and repeat affected verification. Run the final gate against
the actual resulting source, recording hashes, commands, results and skipped work.

Use existing same-host resource evidence as the baseline and measure ordinary
consumers separately from consumers enabling the new features. Record new/replay/
contention runtime cases, variant/envelope costs, setup/migration cost and generated
source/import/build/editor cost. Explain material regressions; do not promise a
performance percentage without comparable measurements. No compile/test workload
runs concurrently with controlled performance measurements.

Update delivered guides, CHANGELOG, exported API documentation and the master with
actual evidence. Replace stale pending statements in touched guides without
rewriting historical acceptance records. Leave unexecuted checks explicit. Mark
this continuation complete only after all rows have direct evidence and the final
re-audit fixes are verified. No publishing, git commit/push/merge or destructive
database operation is part of this continuation.


## Delivered implementation and acceptance

The [team/project fixture](../../tests/fixtures/consumer/teamworkflow/application.go)
uses default `main` and named `receiver` pools, generated models/drafts, scoped
team/project binding, concrete actor callbacks, existing presence wrappers,
`genericdto.Envelope[Action]`, existing numbered pagination and the T06 operation
runner. The named receiver uses a unique existing message-ID receipt and its local
effect in one actual transaction. The [guide](../../docs/guides/typed-api-workflow.md)
describes the source contract. Native acceptance and the complete implementation
[audit](../../docs/guides/typed-api-audit.md) passed, including five confirmed
fixes/improvements, final full verification, independent packaged consumers,
PostgreSQL/races, strict clients, compiler/editor checks, eight fuzz targets and
controlled native cost measurements. The [record](../../docs/evidence/typed-api-t07.json)
retains commands, boundaries and source hashes. No release was published.
