# Second-review acceptance follow-up — 2026-09-29

This follow-up repeats the checks left outstanding by the
[second independent review](second-review-20260929.md). Its runtime and test
inputs matched that revision when these checks completed. The private packaged
consumers and ordinary starter gates passed, but additional Linux queue stress
found one timeout. Release
acceptance remains **incomplete** until that failure is explained and resolved.
The [evidence record](../evidence/second-review-acceptance-20260929.json) preserves
source and package hashes, exact commands, scanner findings, successful gates
and the failed stress run.

## Completed checks

| Check | Result and scope |
| --- | --- |
| `make test-postgres` | Passed in 31 minutes 22 seconds with required PostgreSQL and Redis, race detection and uncached test execution: 167 framework and 103 consumer packages containing tests |
| `make security-check` | All seven checks passed; no affected-function findings, and the TypeScript lockfile audit reported zero findings |
| Bounded fuzzing | All 16 campaigns passed, totaling 4,695,075 executions across prepared messages, configuration, manifests, multipart, WebSocket frames, ETags, compression negotiation, proxy addresses, cookies, webhook headers, datatable requests/XLSX, storage ranges, encryption and pub/sub |
| Private module consumption | All three consumer profiles downloaded from a new private file proxy into an isolated module cache with no local replacements; checksums, reproducible generation, build/runtime checks, targeted full-profile integrations and packaged security scans passed |
| Starter on macOS arm64 | A disposable upgraded copy passed `scripts/verify.py --independent --require-integrations`, including race tests, generated clients, vet, generation freshness, module integrity and compiled CLI checks; ten additional queue-process repetitions passed |
| Starter on Linux arm64 | The same ordinary verification command passed in the existing container image with required integrations |
| Linux deployment | Offline systemd-unit validation and the compiled non-root HTTP process passed readiness, empty-PATH execution, SIGTERM/SIGINT exit and listener-release checks; this does not test live systemd supervision |

The scanner still reports module-only `GO-2026-5932`; none of the three packaged
consumer profiles imports the affected OpenPGP packages. Existing gopls
module-only findings remain recorded separately. This is a scan of the known
vulnerability database, not a claim that all vulnerabilities are absent. All 57
external dependency versions and license/notice hashes match the earlier review.

## Outstanding Linux queue stress failure

The additional ten-repetition `TestJobsOperationsAcrossProcesses` run passed nine
times and failed once at its eight-second wait for the first probe job to reach
the failed state after its normal retry cycle. It reported no data race or panic.
A read of that test's exact retained Redis queue found the job still waiting
with zero attempts and only its initial enqueue transition. It had not been
claimed; this evidence does not identify why the worker made no progress.
Its ready-index entry remains present and its lease index is empty.

Thirty further repetitions with redacted worker-log and job-state diagnostics
passed with the same deadline and assertions. The next campaign added
child-process stack capture on failure and completed 34 successful repetitions
before reaching Go's default ten-minute timeout for the whole command. Its
35th repetition was interrupted during producer setup, before starting a
worker. That harness-budget failure did not reproduce the original job timeout.
A shorter final batch with an explicit overall timeout passed ten repetitions,
bringing the successful diagnostic repetitions to 74. They did not reproduce or
explain the original failure. No runtime change is claimed as a fix, and release
acceptance remains incomplete.

The original output, retained queue state and separate diagnostic patch are
preserved under `.cache/second-review-acceptance-20260929/`. The original stress
command, from the upgraded Linux starter copy, is:

```sh
go test -race ./internal/console -run '^TestJobsOperationsAcrossProcesses$' -count=10 -v
```

`worker-timeout-diagnostics.patch` adds redacted logs, selected job state and a
worker stack dump on timeout. It is separate from the handwritten migration
patch and does not change the eight-second condition deadline or its assertions.

The later [B07 recheck](b07-shutdown-recheck-20260929.md) confirms the correction
for the older CLI publisher shutdown failure and fixes an independent observer
test ordering assumption. Framework runtime behavior remains unchanged. B07
occurred after CLI output; the unclaimed-job timeout above remains a separate
open finding. The later [starter diagnostics follow-up](starter-diagnostics-20260929.md)
records published-version adoption, the additional reported investigation and
verification of the diagnostic patch. B08 still has no established cause.

## Starter migration and rollout

At the time these acceptance runs completed, the real starter remained at
`v0.0.0-20260928094130-4cc5fdeb78b6`. These tested copies used private candidate
`v0.0.0-candidate.secondreview.20260929.1`, with the runtime and generator pinned
together and Go output, manifest version 5, OpenAPI and TypeScript regenerated.
The private candidate is not a published revision.

The handwritten migration patch is retained at
`.cache/second-review-acceptance-20260929/starter-handwritten-migration.patch`.
Its six files still match the earlier stabilization patch: typed login lockout,
declaration-derived migration assertions, explicit scheduler capacity behavior
and the pagination error path. The current starter's core and delivery profile
does not use custom session adapters, datatable downloads, password confirmation
or outbound webhook delivery workers. Applications enabling those features must
apply the additional adjustments in [compatibility](../compatibility.md).

The starter agent has since applied that patch, selected published revision
`v0.0.0-20260929144149-7ee1b7e07e79` for runtime and tool, regenerated contracts
together and recorded passing adoption checks. See the linked follow-up for the
scope and evidence; adoption does not close B08 or complete release acceptance.
Run all configured forward migrations before
starting the release. Stop or drain old Redis queue users before confirmed
layout migration. Follow the documented cache/rate-limit namespace and WebSocket
rolling-deployment rules. Boot never migrates.

No commits, pushes, merges, tags, publication or production migration were
performed. Existing services supplied isolated, retained test namespaces; no
database or Redis data was reset and no dependency versions were added or upgraded. Live
provider-account email, current live S3/R2 certification, production load testing
and deployment remain outside this acceptance. Optional parity features remain
deferred.
