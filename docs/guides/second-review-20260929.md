# Second independent review — 2026-09-29

This review followed the [improvement program](improvement-program-20260929.md)
and the [stabilization review](stabilization-20260929.md). The stabilization pass
was deliberately bounded to authentication, transaction retry, outbox/jobs,
generated contracts and dependency consumption. Here, eight read-only reviewers
re-examined every changed area, then the implementers that own each area fixed
the confirmed defects with regression tests. The per-area "Fixed" sections of the
[changelog](../../CHANGELOG.md) list every correction; rollout rules remain in
[compatibility](../compatibility.md).

## Stabilization corrections confirmed

The row-cancellation hook race, cache load counting, publisher nil receivers,
bounded retry inspection, policy-denial inspection ownership, OAuth trailing-JSON
and RSA-strength checks were re-read and confirmed. A new PostgreSQL test proves
`database.Retry` re-runs serialization failures and deadlocks raised by a
statement inside the transaction, commits only the successful attempt, and never
retries a unique violation.

## Principal corrections

| Area | Corrections |
| --- | --- |
| Authentication | Impersonation cannot mint unmarked tokens or remembered sessions, rotates the actor session in place without extending its absolute lifetime, and ends when the actor's session is revoked or expires. The per-address login ceiling is opt-in. After-hooks veto Before-allowed decisions. Password confirmation can be throttled. Short idle windows no longer log out active users. |
| Database | Invalid polymorphic names can no longer drop the morph type filter. One-of-many through relations pick one row per parent. Pivot sync/toggle serialize on the source row. Sticky reads measure from write completion. Upserts respect global scopes. Relation-based global scopes no longer deadlock. `CreateOrFirst` resolves only the model's own unique conflict. Connections that created temporary objects are discarded. |
| HTTP | Global compression, ETags and browser sessions follow the route's own deadline; late successes are delivered and only credential publications are withheld. Late downloads keep disconnect and shutdown cancellation. Admission rejections are not logged as server failures. The TypeScript client accepts permanent, relative and decorated signed links. |
| Jobs and messaging | Maximum-size workflows with callbacks and migrated layout-1 queues can no longer corrupt a Redis queue. Slow outbox routes no longer roll back their batch. Realtime notification retries only provably unstarted publications. Bulk notification enqueue returns reconciliation IDs. The failure archive keeps original envelope bytes and pages by cursor. |
| Cache and coordination | Stores drain background loads at shutdown. Lease tokens are single-use. Pub/sub supervision survives Redis outages. Hash and set reads sort by bytes independent of server locale. File-lock waits no longer strand threads. |
| Storage and webhooks | HMAC webhook presets derive delivery IDs from signed content. S3-compatible disks require explicit credentials. Server-side copies enforce size limits. Zero-size upload links are rejected. Script-capable attachments are never linked inline by default. Outbound delivery finalization, replay and pruning were corrected. |
| Operations and realtime | WebSocket membership conflicts and mixed-version relays no longer stop a hub. Maintenance allowlists resolve the trusted-proxy client and bypass cookies are sealed with application keys. Public readiness is cached. Startup timeouts are reported as failures. Queue budgets evict the largest slow consumers. |
| Support and tooling | Datatable downloads run under a declared route deadline. XLSX imports bound every token and reject numbers spreadsheets cannot represent. Audit prune is dry-run by default and old history is masked under the current policy. Locale fallback never switches writing system. Spaced generator directives are discovered and unexported enum values fail explicitly. Test helpers replace credentials and factory hooks share the insert transaction. |

## Verification

| Gate | Result |
| --- | --- |
| Area fix checks | Each owning implementer ran gofmt, vet, its package tests and affected consumer fixtures with PostgreSQL and Redis, plus targeted races and repeated timing-sensitive tests |
| `make verify` | Passed in 18 minutes 57 seconds after final regeneration: formatting, vet, 275 test packages, consumer and plugin fixtures, generation, documentation and release tools |
| `make typescript-check` | Strict TypeScript compile and runtime against real HTTP and WebSocket passed, including signed-link forms |
| `make agent-smoke` | Real gopls completion and hover scenarios passed |
| `make race` | The first run found a pub/sub supervision gap reported with an internal cancellation cause instead of the interruption cause. After the fix (a waiting receive now reports the subscription's terminal cause; explicit close keeps cancellation), 1,000 repetitions, `pubsub` races and a full rerun passed: 230 framework and 118 fixture packages in 19 minutes 47 seconds with PostgreSQL and Redis |

The pub/sub correction touched one runtime file after `make verify`; the full race
rerun executed every test package again on the final source. Native checks used
macOS arm64, Go 1.27.1 and the existing local services. Packaged-consumer, starter,
Linux, fuzz and security-scanner checks from the stabilization were not repeated
during this review. The later [acceptance follow-up](second-review-acceptance-20260929.md)
records their reruns, the additional PostgreSQL gate and an outstanding Linux
queue-process timeout.

## Remaining boundaries

Token-family impersonation, a cache failover store, direct `sendfile` from local
storage, lossy WebP, Go-comment schema descriptions, four scaffolds, uncached
settings reads without a scoped transaction and typed message arguments without
a codec round trip remain outside this review. Outbound webhook delivery pruning
is available as `application.PruneWith` because configured assembly does not own
outbound webhook services.
