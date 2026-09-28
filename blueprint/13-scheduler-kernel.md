# 13 — Scheduler kernel

## Purpose and prerequisites

Prerequisites: [09](09-redis-cache-and-coordination.md), [12](12-jobs-and-worker-kernel.md). Reproduce Foundry's scheduled execution with bounded concurrency and distributed coordination.

Rust references: `src/scheduler/mod.rs`, `leadership.rs`, `src/kernel/scheduler.rs`; `tests/distributed_runtime_acceptance.rs`; `blueprints/18-scheduler-hardening.md`.

## Public contracts

Schedules have typed IDs, a validated cron or interval specification, timezone, execution options and a typed handler/job target. Convenience methods compile to the same schedule representation.

Planned usage:

```go
schedule.DailyAt(DailyReport, "03:00", businessTimezone, reportHandler)
```

Strings are parsed once at declaration/configuration boundaries; application code uses validated timezones and schedule descriptors thereafter. Every invocation carries schedule identity, intended occurrence time, execution context and explicit attribution.

## Implementation slices

1. Cron/interval parsing, convenience methods, deterministic next-run calculation and injectable clock.
2. Scheduler loop with bounded concurrent execution, isolated errors and hooks.
3. Redis leadership and per-task overlap leases using the shared coordination layer.
4. Job dispatch integration, environment filters, run history and operational inspection.

## Failure and time semantics

Default to skipping missed occurrences on restart rather than replaying an unbounded backlog. An explicit catch-up option must define a bounded window and maximum occurrences. Document timezone/DST behavior with tests; never silently interpret a local schedule as UTC.

Leadership leases prevent ordinary duplicate scheduling but do not establish exactly-once business effects. Use an occurrence identity plus idempotent target operation. Loss of leadership stops new scheduling; already-running operations follow their cancellation/lease policy.

One failed or slow task must not stop the scheduler or block unrelated due tasks. Reuse the worker timeout guidance: cancellation is cooperative, not a goroutine kill operation.

## Acceptance

Test cron validation, intervals, DST gaps/repeated times, timezone configuration, missed ticks, bounded catch-up, multi-instance leadership, lease expiry, overlap prevention, long tasks, hook failures, context cancellation and shutdown. Tests use a controllable clock rather than real multi-minute sleeps. Apply the [common gate](README.md#common-completion-gate).

## Current implementation

The [scheduler guide](../docs/guides/scheduler.md) owns the concrete public contract.
Implementation, tests and documentation preceded the verification/fix cycle,
following the user's milestone cadence. Native acceptance and consumer review
passed against the final sources on 2026-09-16.

The `schedule` package reuses `temporal` timezone parsing, `lease.Manager`
coordination and typed `jobs` dispatch. `robfig/cron/v3` only supplies parsing and
next-time calculation; existing dependencies had no cron parser. Root/consumer
versions/checksums align. Foundry owns execution, lifecycle, bounded history,
environment filters, catch-up and deterministic occurrence identities.

### Rust source parity review

Inspected `src/scheduler/mod.rs`, `src/scheduler/leadership.rs` and
`src/kernel/scheduler.rs`, including due cursors, leadership renewal, overlap
leases, hooks, environment filtering and shutdown.

| Rust behavior | Go contract |
| --- | --- |
| Cron, intervals and conveniences | Immutable `Spec`, five/six cron fields, explicit timezone, anchored elapsed-time intervals |
| Typed schedule identity/invocation | `schedule.ID`, concrete `Invocation`, typed stable occurrence ID, system attribution |
| Redis owner-token leadership | Shared lease manager/live proof checked before admission; existing Redis coordination reused |
| Non-overlap task leases | Renewed leases retained while canceled callbacks still execute |
| Independent tasks and hooks | Bounded concurrency, panic/Goexit isolation, timeout/cancellation and actual callback drain |
| Per-task environments | Frozen validated filters matched against the manager's namespace environment |
| Due cursors and inspection | Explicit skip/opt-in catch-up, monotonic local cursor, bounded operational snapshots/history |
| Worker integration | Concrete payload builder, stable occurrence-to-job identity and existing queue authority |

Deliberate semantics: no cron year field/inline timezone, implicit startup replay,
durable scheduler history or exactly-once claim. Repeated DST times are distinct
UTC occurrences; nonexistent wall times do not run. Capacity/backlog skips remain
visible. Job publication uncertainty retains occurrence identity. Operational CLI
assembly belongs to milestone 23.

### Verification evidence

Native macOS Go 1.27.1 acceptance passed with required existing PostgreSQL/Redis:
`make verify`, root/consumer vet and tests, 821 compiler-rejection cases,
316 real-gopls completion/hover/definition probes, six behavior-notice probes,
current deterministic generation and documentation checks. The complete gate
passed again after the batched review fix; source hashes matched its snapshot.

Race checks passed for scheduler, Redis, lease manager/memory and the independent
Scheduler kernel consumer. Shared memory/real-Redis tests cover expired owners,
injected coordination failure, leader takeover and overlap renewal while a canceled
callback remains live. Calendar/clock tests cover DST gaps/repetition, missed ticks,
bounded catch-up, clock reversal and capacity/history. Failure tests cover hooks,
timeout/shutdown and panic/Goexit in handlers and custom error inspection.

Consumer review confirms typed IDs/job payloads, explicit business timezone and
injected domain service; no application scheduling loop or lease machinery is
required. Milestone 13 is complete; later milestones and the final whole-framework
audit remain outstanding.
