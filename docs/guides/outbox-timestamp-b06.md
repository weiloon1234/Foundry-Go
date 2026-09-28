# B06: outbox SQL timestamp precision

Status: framework correction verified on native macOS on 2026-09-28. The full
`make verify` gate and the relevant runtime/consumer race checks passed on identical
source snapshots, using required existing PostgreSQL/Redis and tool integrations.
See the [verification record](../evidence/outbox-timestamp-b06-20260928.json).

B06 remains open until the starter's unchanged required independent verification
passes on both macOS and Linux against an available revision containing the
correction. The fix is local and has not been committed or published by this work.

The reported failure against `v0.0.0-20260928063817-2cc081284687` occurs before an
idle publisher can read a row: a nanosecond application clock value reaches the
strict SQL timestamp codec. The correction belongs to the shared publisher, which
also supplies job, event and attachment publication.

## Framework correction

The publisher rounds eligibility and successful completion instants down to SQL
microseconds. It computes a retry deadline from the original completion clock
sample plus the full configured delay, then rounds that deadline up when needed.
It cannot publish a retry before the intended deadline. Already aligned deadlines
remain unchanged; rounding adds less than one microsecond.

The clock, immutable temporal values and caller-value codecs retain their existing
contracts. There is no new public API, generated output, setting or migration.
Applications should continue to use their ordinary configured clock. See the
[outbox guide](outbox.md) for publication and transaction semantics.

Deterministic PostgreSQL tests inject nanosecond clocks on every host and cover an
empty poll, eligibility just before a stored deadline, committed completion,
transient retry, whole/fractional-microsecond delays, one-nanosecond delays, an
already aligned deadline, second rollover and cancellation with rollback/reuse.
The existing ambiguous job publication test also uses a nanosecond clock and
continues to require one accepted queue identity across a lost response and retry.
The codec regression separately proves that explicit nanosecond values are rejected
while generic temporal construction preserves them.

## Starter handoff and closure

1. The repository owner makes the reviewed framework revision available. This
   work does not commit, push, tag or publish it.
2. The starter agent updates the existing single Foundry-Go requirement supplying
   both runtime and tool, following that repository's dependency workflow. Use
   matching generation tooling; do not introduce a second version owner or a local
   clock workaround.
3. Rerun the unchanged `scripts/verify.py --independent --require-integrations`
   on macOS and Linux using the starter's established independent runner, required
   existing PostgreSQL/Redis services and ordinary application clocks.
4. Confirm the original delivery/eligibility/document tests and every later stage,
   including the final compiled console, pass. Record the selected revision,
   terminal results and platform evidence before closing B06.

A passing native framework suite with deterministic clocks proves this precision
boundary without depending on the host clock resolution. It does not substitute
for the required Linux application gate or establish that the published old
revision was fixed.
