# Framework improvement program — 2026-09-29

**Delivered and verified for the source recorded below.** The subsequent
[stabilization review](stabilization-20260929.md) records additional corrections
and independent dependency-consumer acceptance. A framework-wide audit compared
Foundry-Go with the production expectations of a Laravel-style web/API backend and
found operational defects rather than missing core modules: server failures were
not diagnosable, small fail-fast capacity limits turned bursts into 500s, bounded
stores counted dead data and were never pruned, transient infrastructure errors
became permanent or fatal, and several hot paths made avoidable round trips. This
program fixed every confirmed defect, added measured performance work and closed
the main parity gaps. The [changelog](../../CHANGELOG.md) lists each change by area
and the [compatibility policy](../compatibility.md) records the rollout rules.

## Delivered work by area

| Area | Principal changes |
| --- | --- |
| Shared runtime | Redacted `fault.Diagnostic` for every 5xx and error report; panic frames; `fault.Overloaded` (503 with `Retry-After`) and bounded FIFO admission instead of fail-fast `Conflict`; Redis scripts via `EVALSHA`; credential responses withheld after the request context ends. |
| HTTP core | Authorization before validation; model binding before validation; phase-aware 408/503; completed success is never replaced by a timeout; per-route timeouts and body limits; multiple success statuses, typed redirects, raw request bodies and typed server-sent events in the contract, OpenAPI and TypeScript; single-pass JSON response validation. |
| HTTP edge | Lenient parsing of foreign cookies, forwarding chains, origins, user agents and `Accept-Encoding`; CORS patterns; IPv6 /64 rate-limit keys and standard headers; middleware ordering validation; encrypted cookies; typed cache and security headers; pooled compression encoders. |
| Authentication | Shared application authorization registry; per account+IP lockout; pending-MFA caps; single-query session/token lookup with throttled touches; current-credential access and logout; policy hooks and denials; actor-stage middleware and per-user rate limits; events; password confirmation; bcrypt/argon2i import; application key ring with MFA re-encryption; session impersonation; OAuth2/OIDC social login. |
| Database | Leaked rows released with their context; keyset chunking; no savepoint per batch row; array `ANY` membership; count without ordering; server-side timeouts; commit detached from client cancellation; slow-query log and query observer; transaction retry; global scopes; pivot sync operations; set-based writes; relation aggregates, one-of-many, through and polymorphic relations; sticky reads; pruning and confirmed down migrations. |
| Cache and coordination | Live-only capacity with owned pruning; loaded values survive publication failures; detached fills; no caller-wide idempotency lock; tagged operations in one round trip; stale-while-revalidate, request memo, null store, semaphores and typed Redis structures. |
| Background work | Successful handlers stay successful during shutdown; graceful drain with refunded attempts; live-only queue capacity with retained-record bounds; explicit Redis queue layout migration; outbox backoff, requeue and pruning; job middleware, workflows, sync driver, failure archive and queue depth metrics; SMTP LOGIN/XOAUTH2 and failover transports; on-demand and bulk notifications. |
| Storage | Versioned-bucket cleanup; 25 MP image budget; separate read/write/stream admission; listing without per-object HEAD; server-side copy; presigned PUT and POST uploads; S3-compatible providers; attachment image variants; HTTP client retry features; outbound signed webhooks. |
| Operations | WebSocket cluster survives transient Redis failures; lossless log rotation; single shutdown budget with stop delay; public liveness/readiness probes; fleet-wide maintenance mode; Prometheus metrics, OTLP export and pprof; opt-in housekeeping schedule. |
| Support libraries | Batched and context-aware validation rules; audit of changed fields with versioned redaction; resilient datatable exports and CSV/XLSX imports; settings upgrades and cache; money, decimal rounding and number formatting. |
| Tooling | Generator skips cgo packages, drops line numbers from headers, handles enum aliases and checks collisions and versions; cached generated descriptors; tolerant TypeScript decoding; readable schema names; scaffolds, HTTP/database assertions, factories and route documentation. |

## Not delivered

These items were reviewed and deliberately left for later work:

- Token-family impersonation (sessions are supported), Dart clients and the other
  roadmap items in [deferred extensions](../../blueprint/25-deferred-extensions.md).
- A cache failover store (tag and namespace versions differ between backends) and
  typed Redis pattern subscriptions.
- Autocommit for model patches/deletes (inserts only), and skipping the pre-hook
  re-read on hooked writes, both to keep outcome and audit guarantees.
- Direct `sendfile` from local storage readers, which would bypass per-read length
  and checksum enforcement; lossy WebP output (the encoder is lossless only).
- Go doc comments as schema descriptions, nested DTO descriptor references,
  `//foundry:endpoint` generation and test/factory/mail/observer scaffolds.
- Uncached settings reads still use a scoped read-only transaction, and typed
  message arguments still round-trip through their contract codec.

## Verification

| Gate | Result |
| --- | --- |
| Area implementation checks | Each area ran gofmt, vet and its package and consumer tests with PostgreSQL and Redis |
| `make verify` | Format, vet and all 229 framework packages passed with PostgreSQL and Redis. The first fixture check found `http` depending on the scheduler through auth prune helpers; after removing that dependency, fixture, generation, documentation and release-tool checks passed |
| `make typescript-check` | Strict TypeScript compile and runtime against real HTTP and WebSocket passed |
| `make agent-smoke` | Real gopls completion and hover scenarios passed in 254 seconds |
| `make race` | Framework, plugin and consumer packages passed in 22 minutes 24 seconds with PostgreSQL and Redis configured |

Native checks used macOS arm64, Go 1.27.1 and the existing local services. The
separate `make test-postgres` gate, fuzz campaigns, the security scanner and
private package consumption were not rerun for this program; the race gate
already executed the PostgreSQL integration tests. Benchmark figures in the
changelog come from focused before/after runs on the development machine and
state their conditions; they are not throughput or latency guarantees.
