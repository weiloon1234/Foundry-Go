# Stage 11 lane C: measurement preparation

Base `54508185`, on top of the accepted Stage 10 documentation. No Git writes,
public ORM surface changes, budget changes or generator-owner edits.

- The ORM benchmark is already ported in this base. Compared it with main
  `10030ad4eaf6ecf6caf269dfb5e970b0816f366e`: changes are ORM declaration/query/statement
  adapters; workload, scanners, pool sizes, warmup, assertions and timed regions
  remain equivalent. Added its main-side comparison guidance, without a second
  benchmark implementation.
- `tools/goway_compare.py` prepares commands by default; `--run` explicitly runs
  the quiet-host comparisons. ORM uses main's own legacy fixture. Stage 6 requires
  `35f8d45` and selects only ChainBuild, ChainCompile and Hydrate1000. Both build
  binaries first, then alternate five A/B trials with allocation/work metrics,
  source fingerprints, fixed environment and per-trial load averages. Missing
  workloads/failures invalidate evidence. Results report dispersion rather than
  automatically declaring a ±5% pass under noise.
- Footprint mode runs both consumer suites plus branch-only goway budgets. It
  parses counts/bytes and explicitly identifies the draft-to-assignment probe
  migration. It does not manufacture main measurements for branch-only probes.
- `tools/goway_synthetic.py` now accepts a separate old repository, emits current
  typed-role declarations and `rows.Read`, and tests SQL compilation for every
  model. It preserves archive/cache/compiler-RSS evidence and adds build-warmed
  test time and post-test cache size. Measurement order is explicit; repeated
  trials use fresh directories/caches. Existing manifests remain readable.

The [runbook](../guides/goway-final-measurements.md) gives runtime, Stage 6,
footprint and 70-model/40-handler/5-report seed-1 commands and interpretation.
Generation is outside timing. Peak RSS is per compiler, not concurrent process
RSS. Synthetic test time includes compilation/linking with a warmed build cache;
it is not database or application throughput.

## Quick validations

All Go commands used r14, `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

| Check | Result |
| --- | --- |
| Python tooling tests (22) | Pass; native macOS RSS test rerun outside sandbox after `kern.clockrate` was denied |
| Generate 2 models / 1 handler / 1 report, old main and new branch | Pass; new side generates only `state_foundry.gen.go` |
| Both tiny modules: `go test -count=1 ./...` | Pass; model SQL assertions executed; app/handler/report compiled |
| ORM compile benchmarks, one iteration, no PostgreSQL | Pass, both list and filters |
| Stage 6 branch benchmarks, one iteration each | Pass, including complete hydration and one-query checks |
| ORM and footprint dry plans against pinned main | Pass; no benchmarks or footprint suite executed by these plans |
| Stage 6 dry-plan baseline/command selection | Pass in isolated unit test; no pre-4a checkout was created |
| `make docs-check`, gofmt on benchmark fixture, `git diff --check` | Pass |

Initial host load averages were 29.89 / 24.87 / 30.13 with other work active.
Single-iteration and tiny-module results are functional checks only. No latency,
footprint or build-resource improvement is claimed. No PostgreSQL, cold-cache,
full footprint or 70-model measurement ran. Those paired measurements need the
quiet-host session, prepared pinned main/pre-4a checkouts and private PostgreSQL
configuration described in the runbook.

Local tiny artifacts: `/private/tmp/goway-stage11-tiny`. Final dry plans:
`/private/tmp/goway-stage11-orm-final-plan/comparison.json` and
`/private/tmp/goway-stage11-footprint-final-plan/comparison.json`.
These are preparation artifacts, not gate passes.

## Remaining generator dependency

Only the synthetic **old** side needs generated model/projection APIs and must
run against main. The new side requires only retained enum generation and works
without model/projection directives. No Stage 8c generator files were edited.

## Shared files touched

`tools/goway_synthetic.py`, `tools/goway_synthetic_test.py`,
`tests/fixtures/consumer/orm_benchmark_test.go` (comment only), and
`docs/guides/orm-performance.md`. The comparison runner/tests, runbook and this
slice's evidence are new files. Makefile targets and footprint ceilings are unchanged.

## Docs notes

Suggested lead-owned status: Stage 11 measurement tooling is prepared and tiny
validation passed; runtime, Stage 6 paired timing, footprint and whole-app gates
remain pending quiet-host execution. Do not treat this preparation as final
performance acceptance or replace historical evidence with smoke timings.
