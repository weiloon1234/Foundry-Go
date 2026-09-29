# B07 jobs CLI shutdown recheck — 2026-09-29

The B07 handoff describes a real failure on the starter's pinned framework
`v0.0.0-20260928094130-4cc5fdeb78b6`: `jobs failed --format json` could produce
valid output, then fail while its managed outbox publisher stopped. The
shutdown condition is **already corrected in the current framework**. No further
runtime fix or B07-specific starter migration is required by this recheck.
Adoption was pending when this recheck completed. The later
[starter diagnostics follow-up](starter-diagnostics-20260929.md) records the
published-version adoption and closure of B07 in the starter; B08 remains open.

## Why the correction applies

The old publisher required its transaction error to match the stopped managed
context. A driver could instead return a typed `Unavailable` or
`DeadlineExceeded` error without Go cancellation identity. That error escaped the
managed task even though the CLI had completed and application shutdown had
cancelled the task's own context. The caller's context could remain active.

The current publisher ends its managed loop when that loop's context stops.
While it remains active, publication failures retain the existing redacted
diagnostics and bounded backoff. This lifecycle result does not claim a
publication committed: `PublishOne` and `PublishBatch` preserve transaction
errors, and their results are marked committed only after successful transaction
completion. An uncertain outcome is never converted to confirmed rollback or
permission to repeat business work.

## Verification

The [evidence record](../evidence/b07-shutdown-recheck-20260929.json) contains
commands, source identities and terminal results.

| Check | Result |
| --- | --- |
| Controlled old-behavior check | A temporary Go overlay restoring the old shutdown cancellation test made the existing deterministic regression fail for unavailable, deadline and commit-unknown classifications; checkout runtime files were untouched |
| Current shutdown regression | 1,000 race-enabled repetitions passed, including preservation of one-shot publication failures |
| Publisher, foundation, PostgreSQL and CLI races | Passed with required existing integrations |
| Original B07 diagnostic probe on macOS arm64 | Five unchanged 100-call batches passed against the private packaged candidate: 500 short-lived `jobs failed` CLI calls |
| Original B07 diagnostic probe on Linux arm64 | The same five batches passed: another 500 calls, using the existing container and services |

The first negative-control overlay had an unused-variable compile error. That
harness attempt is retained separately; only the corrected overlay reproduced
the regression. The wider database race suite also exposed an independent test
ordering assumption: query cancellation asynchronously closes rows, so a later
observer-scope call can return either cancellation or `Closed`. The fixture now
accepts those two valid rejection outcomes while still requiring no callback
invocation and no retained owner. Its 1,000 race repetitions and the full
database race suite then passed. This was a test-only correction.

The probes used private candidate `v0.0.0-candidate.secondreview.20260929.1`
without a local replacement. Its runtime still matches the current framework;
the observer test correction does not change runtime or generated APIs. The
existing [acceptance follow-up](second-review-acceptance-20260929.md) records the
complete macOS/Linux starter gates on this runtime. Its separate Linux failure
occurred before the first job was claimed, before any `jobs failed` CLI check.
That unresolved timeout remains open and prevents complete release acceptance.

## Starter handoff

The six-file handwritten migration patch for the pre-upgrade starter is unchanged:
`.cache/second-review-acceptance-20260929/starter-handwritten-migration.patch`.
It has since been applied by the starter agent; do not apply it a second time.
Adding CLI error suppression, command retries, larger condition timeouts or
disabling publishers would conceal failures and is unnecessary for B07.

The adoption checklist used for this handoff was:

1. Prepare the existing migration patch. When a reviewed revision is published
   and selected, pin it for both runtime and `foundry`, then regenerate Go,
   manifest version 5, OpenAPI and TypeScript together.
2. Run the original jobs process test repeatedly and the supplied B07 probe in
   a disposable copy, with required PostgreSQL/Redis. Keep its existing clocks,
   publisher settings, timeouts and privacy assertions.
3. Complete `scripts/verify.py --independent --require-integrations` on macOS
   and Linux, including the final console and Linux deployment checks. Record
   the actual new module version and results before updating the starter's B07
   gap and final-quality-review status.
4. Track the unclaimed-job timeout separately. The acceptance follow-up retains
   `worker-timeout-diagnostics.patch` for a disposable diagnostic copy; it adds
   state/log/stack capture without changing the eight-second job condition.

This recheck did not edit the real starter, commit, push, tag or publish. The
starter's subsequent adoption is recorded separately; no migration patch change
was needed for this B07 finding.
