# 24 — Production hardening and release

## Purpose and prerequisites

Prerequisites: milestones 01–23. Close the parity map with production evidence, operational documentation and a coherent compatibility policy.

Rust references: `src/logging`, `src/foundation/doctor.rs`, `src/kernel`, `tests/observability_acceptance.rs`, `websocket_observability_acceptance.rs`, `distributed_runtime_acceptance.rs`, `docs/release-checklist.md`, `blueprints/16-production-hardening.md`.

## Public and operational contracts

Complete typed liveness/readiness probes, metrics/traces, protected runtime diagnostics, error reporting, maintenance mode and resource-limit configuration. Feature packages contribute observations through shared contracts rather than each running a separate diagnostics server.

Planned probe shape:

```go
func DatabaseReady(ctx context.Context) error
```

Liveness reflects process health; readiness reflects configured dependencies needed to serve traffic. A dependency failure must not automatically make an otherwise healthy process fail liveness and trigger an endless restart cycle.

## Implementation slices

1. Reconcile every row in the master parity map against Go source, docs and acceptance evidence. Explicitly document omissions instead of declaring untested parity.
2. Complete request/job/schedule/socket tracing, correlation IDs, metrics, redaction and pluggable error reporting.
3. Validate startup/shutdown and maintenance behavior across all five kernels and partial dependency failure.
4. Run bounded-load, race, fuzz and fault-injection suites. Establish published benchmark conditions and resource budgets instead of unsupported performance claims.
5. Establish API/module/manifest/protocol compatibility rules, release checks, migration guidance and operational runbooks.

Include developer tooling in the resource measurements: cold and incremental generation/build times, peak compiler memory, generated-code size, and gopls completion/hover latency in representative independent consumers. Track growth as generic query scopes and model APIs expand. Measure ordinary consumer builds with default compiler flags separately from the full framework acceptance suite and its ordinary/race caches; fixture-wide compilation cost is not an application runtime benchmark. Record constrained-VM build profiles separately rather than using them as evidence that default consumer builds are acceptable. Milestones 06/07 verification exposed compiler-memory, debug-metadata and cache-capacity limits in the shared 4 GiB/20 GiB development VM, so these measurements must inform the final developer-experience audit.

Complete PostgreSQL read/primary routing from Rust's `src/database/runtime.rs` and database guide. The accepted implementation owns explicitly configured primary and optional read pools. Add typed configuration for optional read and primary pools, explicit primary reads, primary-owned transactions/migrations/seeders, separate health observations, bounded combined connection budgets and coordinated shutdown. Avoid choosing an execution pool by guessing from SQL text. Verify routing and replica-unavailable behavior with independent connector fixtures, and preserve the actual transaction executor through model, job and realtime work. Replica reads must not promise immediate visibility of primary commits.

Audit generated persisted-field coverage against the runtime codec inventory. The milestone 07 source review found that runtime byte codecs exist while model discovery rejects `[]byte` fields. Complete generated binary-field support, including typed nullable values, supported comparisons, mutation inputs, fresh generation, PostgreSQL persistence and consumer/LSP evidence, before declaring full codec coverage. Do not treat a runtime codec alone as generated-model support.

## Failure and rollout behavior

Protect diagnostics with normal auth/policy APIs and minimize sensitive data. Graceful termination must stop new work before closing shared dependencies; bound draining and report incomplete work clearly.

Test compatible application/worker versions during rolling deployment: queued payload versions, outbox records and realtime contract changes need explicit handling. Schema changes remain forward-reviewed migrations, never automatic destructive synchronization.

Provider/client dependencies require version and security review. Release packaging must build in an independent consumer without local replacements or sibling Rust files. Publishing, Git commits/pushes/merges and production deployment remain user-controlled actions.

## Acceptance

Require unit/behavior tests, compile-pass/fail tests, reproducible generation, consumer/plugin builds, PostgreSQL/Redis integration, S3/R2 certification, HTTP/realtime/SDK contract tests and documented resource/failure results. Verify credentials are absent from logs/artifacts and examples match the released surface.

The first complete release contains the agreed PostgreSQL-first, local/S3/R2, Rust-parity realtime and TypeScript feature set. It does not imply completion of [deferred extensions](25-deferred-extensions.md).


The milestone 07 reference integration additionally measured a complete query race build at 3759.4MiB peak child RSS in the 4GiB VM. The initial compiler process was killed with GOGC=25/GOMEMLIMIT=256MiB; the retry passed with GOGC=10/GOMEMLIMIT=192MiB and the existing package-specific compiler flags. This is constrained verification evidence, not default-toolchain developer-experience acceptance. The final production gate must address the near-limit compiler working set and verify ordinary consumer/gopls use under documented hardware budgets.

## Accepted implementation

Milestones 01–24 are accepted. The complete implementation batch passed native
verification, followed by one complete-framework audit, grouped fixes/races and
the full post-audit gate. Independent packaged consumers, controlled native
build/generation/editor measurements and dependency/security/license review
passed. Explicit primary/read routing and generated binary ownership have
connector, PostgreSQL, generator, compiler, consumer and editor evidence.

See [production acceptance](../docs/production-acceptance.md),
[parity reconciliation](../docs/guides/parity-reconciliation.md),
[measurements](../docs/guides/developer-resource-measurements.md),
[release checks](../docs/release-checklist.md), [compatibility](../docs/compatibility.md)
and [dependency review](../docs/dependency-review.md) for actual results and
limits. Historical S3/R2 evidence is retained for unchanged adapter source;
optional real-account email sends remain unverified. Milestone 25 stays deferred.
Publishing, Git operations and deployment remain user-controlled.
