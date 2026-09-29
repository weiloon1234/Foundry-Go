# Scheduler kernel

In configured applications, use `s.Calendar().DailyAt(id, "09:00", handler)` or
its `Cron`, `Hourly`, `Daily`, `Weekly` and `Monthly` helpers to inherit
`Settings.TimeZone`. `s.Calendar().In(zone)` returns an explicit override.
See [application timezones](application-timezone.md). Existing package functions
continue to accept an explicit location, and interval schedules measure elapsed time.

Milestone 13 passed native acceptance and consumer review. The
[roadmap](../../blueprint/00-master-architecture-and-parity.md) owns acceptance
status. The [independent consumer](../../tests/fixtures/consumer/scheduling/)
demonstrates declarations, injected domain services and kernel assembly.

## Declare timing once

```go
const DailyReport schedule.ID = "reports.daily"
zone, err := temporal.ParseTimeZone("Asia/Kuala_Lumpur")
// Handle err before declaring the schedule.
declaration, err := schedule.DailyAt(DailyReport, "03:00", zone,
    func(ctx context.Context, invocation schedule.Invocation) error {
        return reports.BuildDaily(ctx, invocation.IntendedAt)
    })
```

Handle returned errors. Declarations freeze timing/options; `With(options)` returns
another validated declaration. `NewRegistry` rejects duplicate IDs and invalid
handlers/configuration. Applications provide domain behavior; Foundry owns runtime.

`ParseCron` accepts five fields (minute first) or six (second first). Year fields,
embedded timezone prefixes and `@` directives are rejected. Locations must be
explicit; nil, empty and host-dependent `time.Local` are rejected. Resolve strings
with `temporal.ParseTimeZone`. Parsing uses
[robfig/cron](https://pkg.go.dev/github.com/robfig/cron/v3); Foundry owns execution.

`Cron`, `Every`, `Hourly`, `Daily`, `DailyAt`, `Weekly` (Monday) and `Monthly`
compose one `Spec`. `DailyAt` requires `HH:MM`. `Interval` aligns elapsed-time
periods to the UTC Unix epoch; `IntervalFrom` supplies an explicit first instant.
Periods and anchors require whole milliseconds, with periods between one
millisecond and 365 days. Handler duration/restarts do not cause drift.

`EveryMinute`, `EveryFiveMinutes`, `EveryTenMinutes`, `EveryFifteenMinutes` and
`EveryThirtyMinutes` run on local wall-clock minute boundaries in an explicit zone
(also on `Calendar`). `LastDayOfMonthAt(id, "HH:MM", zone, handler)` runs on the
last local day of each month.

Options narrow a declaration without new timing syntax. `Days` (for example
`schedule.Weekdays()` or `schedule.Weekends()`), `Between` (built with
`schedule.Between(from, to)` or `schedule.UnlessBetween(from, to)` from
`temporal.Time` values; a window wraps midnight when `from` is after `to`) and
`LastDayOfMonth` are evaluated in the spec's zone (UTC for intervals) at
admission. A filtered occurrence advances the schedule silently, like a time the
spec never produced: no history, log or execution slot. `When` is an owned
predicate evaluated immediately before the `Before` hook; `false` records the
occurrence as skipped with reason `filtered` (not logged as a problem), and an
error fails it as a hook failure. `EvenInMaintenanceMode` keeps admitting a
schedule while the application's maintenance gate is paused. Use `After` and
`Failed` for success and failure callbacks. Start from `declaration.Options()`
when calling `With`, so helper-set options such as `LastDayOfMonth` are kept.

`Spec.Next` returns a strictly later UTC instant. DST gaps skip nonexistent local
times; repeated wall times produce distinct UTC occurrences and IDs. Intervals
measure elapsed time independently of DST. Restricted day-of-month/day-of-week
use cron OR semantics; a wildcard leaves the other restriction active. Unreachable
dates are rejected. Future calendar search is bounded to five years and returns
`fault.Missing` when no future instant exists in its supported range.

## Runtime and ownership

Construct `schedule.New(manager, registry, schedule.DefaultConfig(group))` with
an existing `lease.Manager`. Construction starts no I/O or goroutines. Its namespace
owns application/environment isolation; typed `schedule.Group` separates elections.
Replicas of a group must use the same declarations and policy.

Use the existing Redis client as lease backend for multiple processes, or explicit
`lease/memory` for local tests. There is no automatic memory fallback. Acquisition,
renewal and owner-conditional release reuse the shared lease layer.

`Run(ctx)` is single-use and drops caller context values. Invocations receive
system attribution from the schedule ID, intended UTC time and a stable typed
occurrence ID. `schedule.Current(ctx)` only represents live owned callbacks;
saved contexts stop representing execution after callbacks exit.

Cancelling `Run`'s context (the kernel shutdown path) or calling `Drain(ctx)`
starts a graceful drain: admission stops while running tasks keep leadership and
their overlap leases for up to `Config.DrainTimeout` (default 5s); the deadline
then cancels them. `Stop(ctx)` stops admission and cancels work immediately. Both
bound only the caller's wait. Application assembly rejects a `DrainTimeout` that
is not shorter than `ShutdownTimeout - StopDelay`. `Done` closes after actual
callback exit. A context-ignoring goroutine retains its slot; Go cannot kill it.
Self-wait from an active callback returns `fault.Cycle`.

`Config.Logger` (the application logger under `schedule.Module` when nil)
receives `schedule occurrence skipped` at WARN for `missed`, `capacity_reached`,
`backlog_limited` and `overlap_busy` decisions and `schedule invocation failed`
at ERROR, with schedule ID, occurrence ID, intended time, reason and a redacted
diagnostic (type names, framework fault codes and panic frames, never error
text). Invocation observations end with the same diagnostic.

`Scheduler.RunNow(ctx, id)` invokes one registered schedule immediately, outside
leadership and timing, and returns its classified record: the `When` predicate,
hooks, timeout and overlap protection apply, calendar filters and catch-up
cursors do not. Register `schedule/command.Declaration` with a constructor
returning `services.Scheduler()` to expose it as
`./service schedule test --id reports.daily [--format json]`; the command
prints only the classification and fails unless the invocation succeeded (or its
`When` predicate declined it).

`schedule.Module` registers the typed service and foundation Scheduler kernel.
List the lease manager's owning provider in `requires`. Only running the Scheduler
kernel starts scheduling. Keep leases/backend alive until Done; declared provider
dependencies enforce this shutdown order through foundation.

## Missed occurrences and capacity

Startup and leadership acquisition skip missed occurrences by default, including
one exactly at the initial cursor. During operation, `Config.Grace` bounds lateness;
older occurrences are skipped with history. Match polling/grace to required precision.

Opt into `Options.CatchUp` with both `Window` and `Max`: at most seven days and
1,000 considered occurrences per schedule per tick. When the backlog exceeds
`Max`, the most recent occurrences run and the older ones are recorded as one
`backlog_limited` skip. Catch-up resumes after a per-schedule cursor of the last
completed or deliberately skipped occurrence. When the lease backend implements
`schedule.CursorBackend` (the Redis coordination client does), the cursor is
persisted in the coordination store, so a restart or leadership change does not
replay occurrences another process already completed; otherwise it lives only in
the scheduler process. A cancelled occurrence (shutdown, leadership loss) is not
recorded, so catch-up may replay it.

`MaxPerTick` bounds total admissions/skip decisions; a rotating schedule cursor
gives due schedules turns. When all `Concurrency` slots (default 16) are busy, a
due occurrence waits up to `Config.CapacityWait` (default 5s) for a slot and is
then skipped as `capacity_reached`, advancing its cursor. Scheduling is best
effort, not durable replay; use a domain ledger if every occurrence must
eventually complete.

`Config.Clock` injects application time. Optional `Wake` replaces the polling ticker
for deterministic tests; callers own the channel and closing it terminates Run
with an error. Leases, timeouts and shutdown use real safety time independently
of a frozen application clock. Clock reads must be concurrent-safe and nonblocking.
Backward readings are clamped within one scheduler lifetime to prevent local replay.

## Overlap, failure and inspection

`Options.WithoutOverlap` uses a renewed schedule lease within group/namespace.
Local active executions are also checked. Leadership loss stops new admissions
and cancels that epoch's callbacks. Overlap leases continue renewing until actual
callback exit, including after timeout/cancellation. Unknown renewal outcomes or
lease loss cancel work. Stale-process effects still require idempotency or resource
fencing; leadership cannot promise exactly-once effects.

`Before`, handler and `After` run in order; failure prevents later success stages.
`Failed` receives the in-process error, including panic/Goexit or timeout. Hook
failures are joined and isolated. Hooks share the timeout and remain owned until
exit. Outcome classification also isolates custom error inspection methods and
bounds traversal to 256 nodes and 64 unwrap levels. If a panic marker cannot be
established within those bounds, the invocation keeps its ordinary handler/hook
failure classification and releases its execution slot. Custom methods must return;
domain errors do not enter lease coordination classification. Failed invocations
do not stop unrelated schedules; there is no automatic
business callback retry for that occurrence.

`Snapshot` copies leadership/concurrency counters, enabled schedules/next cursors,
and bounded local history. History contains classifications, not arbitrary error
or panic text, and is not a durable ledger. Long-running callbacks remain active
if their initial history entry is evicted; completion is recorded within the same
bound. Authorize operational endpoints exposing attribution/occurrence IDs.

## Typed job targets

`schedule.JobTarget(dispatcher, definition, payloadBuilder)` returns an ordinary
schedule handler while preserving the job payload type. Derive the same payload
for each occurrence. Its ID becomes the queue execution ID for deduplication across
unknown acceptance/failover while retained. Repeated DST UTC instants stay distinct.

Accepted jobs use normal queue retries/timeouts. Publication failure is a schedule
failure, not proof of queue rejection. Queue retention must exceed catch-up and
publication recovery windows. Changing payload/definition for a retained occurrence
can conflict; version schedule IDs deliberately. The handler context carries
system attribution into captured job envelopes.

## Verification contract

The gate covers parsing/DST, missed ticks/catch-up, concurrency/history bounds,
hooks/shutdown, local and real-Redis leadership/failover/overlap, job identity,
independent consumer/compiler/editor checks, races, generation and full `make verify`.
Executed evidence is recorded in the roadmap. Redis fixtures use isolated keys and
existing services; no database reset or additional server is required.

Lease coordination errors use bounded inspection too. Failed overlap cleanup is classified inside owned isolation, records a terminal coordination failure and releases scheduler capacity. A reached ownership-loss marker retains its existing meaning; leadership error searches cannot loop over an adapter error cycle.
