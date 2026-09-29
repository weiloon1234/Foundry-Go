# Starter adoption and timeout diagnostics — 2026-09-29

The starter has adopted published revision
`v0.0.0-20260929144149-7ee1b7e07e79`. Its recorded independent macOS/Linux
acceptance, ten original queue-process repetitions per platform, 500 B07 CLI
calls per platform and Linux deployment checks passed. Those adoption runs were
performed by the starter agent. This follow-up checked its seven successful
result files and matching log hashes, and independently compared all 1,553
packaged non-test Go/TypeScript source
files with the previously tested private candidate: they match.

**B07 is closed in the starter. B08, the earlier unclaimed-job timeout, remains
open.** Published-version adoption and subsequent successful runs do not explain
the original failure. Overall release acceptance remains incomplete; this
follow-up does not grant a release exception.

## Additional investigation

The user supplied a second review reporting 20 successful normal Linux runs
(claim 55–65 ms; failed state 222–258 ms) and 12 successful CPU-saturated runs
(claim 0.17–1.06 s; failed state at most 1.45 s). It also reported clean service
logs around the original event and 6,000 fast later lookup/connection probes.
These are attributed observations: the reviewer's scratchpad location was not
supplied, so this follow-up has not inspected its raw timing and probe logs.

Those results make sustained CPU saturation a weaker explanation. They do not
rule out a transient stall in the original worker. Successful Redis reads in
the parent process also do not establish progress of a new worker's database
connection. A startup connection stall remains a hypothesis, not a finding.

Source inspection confirms that database startup retries are silent. The default
connection attempt timeout is five seconds and the startup retry window is
15 seconds. The retry window is not a hard total duration: an admitted attempt
still has its own connection timeout. No startup logging or timeout behavior
was changed. Safe per-retry warnings remain a separate optional improvement.

## Remaining starter change

The six-file handwritten migration patch has already been applied unchanged.
The only remaining proposed change is the existing one-file diagnostic patch:

```text
Foundry-Go-Starter/docs/evidence/b08-worker-timeout-diagnostics.patch
```

It is byte-identical to the framework's retained
`.cache/starter-diagnostics-20260929/worker-timeout-diagnostics.patch` and applies
cleanly to the current starter. It prints selected job state and captured worker
logs on timeout, requests a worker stack dump, and removes the configured test
database/Redis password strings. The job payload is not printed. This is not a
general-purpose sanitizer for arbitrary application log content.

The existing eight-second condition, polling interval, publisher settings,
success assertions and privacy checks remain intact. Stack capture adds a short
diagnostic wait only after the condition has failed. Apply this patch to the real
starter test through the starter agent; there is no need to regenerate contracts
or reapply the six migration files. Keep the original failure record and B08
open until evidence identifies its cause, or record an explicit operator release
exception separately.

## Targeted verification

The [evidence record](../evidence/starter-diagnostics-20260929.json) retains
commands, source hashes, results and limits. Verification uses disposable starter
copies with the previously tested private candidate, whose compared runtime
sources match the published revision. The actual starter is read-only in this
follow-up; its own full published-version gates are recorded separately.

| Check | Result |
| --- | --- |
| Incremental patch against current starter | Applies cleanly; identical to the starter-owned B08 patch |
| Controlled startup stall | Expected test failure captured waiting state, zero attempts and the worker stack; configured test credentials and private error canaries were absent |
| Normal queue-process tests with diagnostics | Three race-enabled repetitions passed on macOS and three on Linux, with the original eight-second condition |

The initial attempt to check a combined migration/diagnostics patch failed
because the starter agent had already applied the six migration files. That
stale-baseline check is retained; the incremental one-file patch passed.

The controlled failure blocks only the test worker immediately before runtime
startup through a Go overlay. It verifies diagnostic output for a synthetic
startup stall; it cannot establish the cause of the historical B08 failure.
