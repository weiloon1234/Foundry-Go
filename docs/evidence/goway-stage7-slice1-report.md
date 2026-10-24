# Stage 7 slice 1: outbox store

Base: `5cbe31dce4fe3c3e1aacef402ad637d161a87590`. No commit.
Updated: 2026-10-23T19:50:00+00:00.

## Migration

Migrated `internal/outboxstore` and its publisher callers to `orm`. The handwritten
`Messages` declaration owns the existing `Message` fields and codecs. Enqueue,
address-scoped reload, publication claims/outcomes, requeue, failure inspection,
state counts and bounded pruning use the existing ORM and expression APIs.

Public event/job/outbox APIs, stored table/column names, UUID ownership, JSON
payload/origin codecs, database defaults and nullable publication time are unchanged.
Claims retain their ordered `FOR UPDATE SKIP LOCKED` query. Pruning retains one
bounded DELETE with an IN subquery; it never hydrates payloads. Business transactions,
per-write savepoints, retry deadlines and unknown-commit outcomes keep their owners.

`make generate GENERATION_DIRS=internal/outboxstore` removed the obsolete generated
model through the publisher and retained its empty ownership manifest. The directory
was then removed from `GENERATION_DIRS`; full `make generate` used the normal order.
No generated file was edited by hand. No dependency, public ORM method, capability
bridge, surface snapshot or compile-fail catalogue row was added or changed.

## Parity and footprint

Ten frozen driver transcripts were captured against the exact branch-base sources
using a Go overlay, then replayed against the migrated implementation. They compare
SQL, bindings, savepoints and transaction counts for append, scoped find, successful
publication, retry, attempt exhaustion, unknown commit, requeue, prune, failed rows
and stats. UUID entropy alone is normalized and its UUIDv7 shape is checked. Tests
also assert hydrated defaults and publication/reconciliation results.

Statement counts, excluding savepoint commands: append/find/prune/failed = 1;
publication/retry/exhaustion/unknown/requeue = 2; stats = 3. Unknown commit makes one
publication attempt and never marks the candidate committed or retries it.
Existing PostgreSQL integration sources remain the persisted-state/locking tests;
this lane compiles/vets them only.

Two new footprint entries measure the actual framework packages, not synthetic
copies: outboxstore 2,896 functions (ceiling 3,331), publisher 1,324 (ceiling 1,523).
Each ceiling is `ceil(measured × 1.15)`; no existing ceiling changed. These are
archive symbol counts, not runtime performance claims. All 73 probes pass.
The branch-base overlay confirms no growth for `q_with` (1,211/1,211) or
`aggregate_families` (4,362/4,369).

## Remaining stores

Only outbox meets this slice's complete, no-conflict migration boundary:

- MFA: `factors(scope)` requires predicate-preserving keyed Update/Delete. This
  HEAD offers table-keyed ordinary writes, not the corresponding scoped adapters.
  Replacing them would discard the scope predicate or change lifecycle/cardinality.
- Countries, jobarchive, idempotency, challenge, session and token: conflict/upsert
  consumers are deferred by this slice's explicit boundary. Basic 6e1 policies are
  present; these are not all waiting for advanced 6e2. Auth stores also need scoped
  write adapters; preserve their existing handwritten maintenance SQL.
- Notifications: conflicts plus bounded per-model update/delete adapters.
- Webhooks: ordered UUID seeks and predicate-preserving keyed writes.
- Extensionstore: owner/slot adapters, conflicts, bounded per-model/source writes.
  Locked row projections are now available in 6c1 and are no longer the blocker.
- Attachments: owner/slot adapters and ordered UUID seeks; preserve reconciliation.
- Auditstore: bounded source deletion. B5d capture is now present.

Datatables, model binding, factories, identity adapters and broader application
integration remain later Stage 7 slices. No replacement adapters were improvised
inside a store.

## Acceptance

Final scoped acceptance passed:

- `go build ./...`; vet of database/internal/outbox/events/jobs and both fixtures.
- Race tests for ORM/query/querycore/ormcore/outbox/events/jobs and the affected
  event/observer/hook/client-contract consumer packages.
- Go-way fixture tests, including compiled/vetted PostgreSQL smoke sources.
- Full Makefile generation and generation freshness, catalogue and docs checks.
- The branch-base golden oracle and baseline/current contract exports: nine
  artifacts byte-identical, including manifest, OpenAPI, TypeScript and React/Vue.
- Formatting and diff whitespace checks.

The initial fixture run failed only the two new footprint baselines: a preliminary
counter missed generic symbol names containing spaces. The existing harness supplied
the correct measurements; the full footprint test then passed. All pre-existing
budgets retained their exact values. Test-harness setup corrections are recorded in
evidence. No contract source changed, so `make typescript-check` was not required;
the client-contract race tests and fresh export comparison still ran.

The accompanying [evidence JSON](goway-stage7-slice1.json) records the final commands,
statuses, source hashes, contract export comparison and footprint measurements.
All Go commands use `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2`,
`GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`. PostgreSQL/Redis opt-in variables are
unset for acceptance; no live service acceptance is claimed.

## Shared files

`Makefile`; `docs/guides/outbox.md`; `tests/fixtures/goway/footprint_test.go`;
`events/outbox_failures_postgres_test.go`; `outbox/publisher/publisher.go`,
`operations.go`, `timestamp_postgres_test.go`; `internal/outboxstore/message.go`,
`store.go`, `.foundry-gen.json`, and the generator-removed `message_foundry.gen.go`.
New files are the parity test, ten transcript fixtures and this report/evidence.

## Docs notes

Lead-owned docs were not edited. Record one of thirteen framework-store packages
migrated: outbox, including event/job enqueue and the shared publisher. State the
MFA scoped-write blocker explicitly; the earlier inventory's “closest to ready”
was not proof that its entire write path was supported. Preserve the remaining
Stage 7 work, live PostgreSQL acceptance and Stage 8 generator retirement as open.
