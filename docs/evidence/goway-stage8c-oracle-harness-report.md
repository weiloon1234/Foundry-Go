# Stage 8c oracle harness — final review corrections

All three findings in `last-32.md` are addressed. The final freeze is still pending;
no legacy or generated code has been retired.

- **Snapshot fidelity:** model-only filtering stops at model boundaries; scalar
  internals remain structural. Regressions distinguish typed IDs, decimals,
  microsecond timestamps, nullable scalars, relation aggregates and nested related
  IDs, while still ignoring private/nonpersistent model fields. Raw snapshots keep
  their full structure.
- **Filtered replay:** expected records come from frozen package/test identities
  and Go's `-run`/`-skip` selection, never the consumed set. Selected parents,
  subtests and zero-observation tests must consume every occurrence. Tests compare
  the selector to real Go test-runner selections. Package identity also prevents
  the goway root from claiming its integrations package's records.
- **Census:** pagination cursor parsing/type identity and PostgreSQL model
  hydration/aggregates now record and replay. The self-sweep additionally captured
  two statement-detector comparisons and the PostgreSQL mutator email comparison.
  The reviewed census has 489 fixture sources and 84 paired entry points in 27
  packages (24 consumer packages). Full selection has 119 assertion sites; two
  PostgreSQL sites are excluded explicitly from the offline check.
- **PostgreSQL path:** `--check --postgres` captures and replays with required
  PostgreSQL mode. Both publication commands require `--postgres`. Stable fixture
  seed IDs make model reads repeatable across isolated schemas; the recorder still
  records actual legacy query output. The lead runs this path. An offline check
  against published evidence may omit only registered PostgreSQL tests, including
  shared-helper cases; disappearing ordinary records still require refreezing.

The retained refreeze isolation, receipt guard and fixed build provenance remain
at the shared `tests/oracle` owner. No production API or surface additions,
Query/Table methods, catalogue ports, dependencies, generator outputs or footprint
ceilings changed. Shared files touched for this correction are the oracle package,
freeze command, census/guide, goway TestMain and statement tests, integration
pagination tests, consumer model parity tests and queryfixture seeding, and the
PostgreSQL mutator smoke test/TestMain.

## Self-sweep

Seven framework files still contain live legacy/current comparisons outside the
fixture census: datatable projection goldens, complete-record iteration, keyed
writes, extension/identity adapters, pagination contracts, plan contracts and
column-kind signatures. The [comparison inventory](goway-stage8c-oracle-comparison-inventory.md)
names every test and separately lists retained cross-API persistence/behavior tests
and store goldens already frozen elsewhere. This is not a claim of a complete-tree
freeze; those remaining comparisons need the lead's retirement disposition.

## Acceptance

Final targeted results and source hashes are recorded in the
[evidence JSON](goway-stage8c-oracle-harness.json). All commands use r12,
GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.

| Check | Result |
| --- | --- |
| Temporary capture and complete selected replay | PASS: 840 golden observations + 12 footprint records; 117 non-PostgreSQL sites |
| Harness/command build and targeted vet | PASS |
| Harness, command, goway, integrations and 24 consumer-package races | PASS (`-count=1`) |
| PostgreSQL sources | Compiled/vetted only; live capture belongs to the lead |
| Catalogue, census hashes, gofmt and diff whitespace | PASS |

Documentation-link validation is recorded separately in the evidence JSON.

The PostgreSQL sources are compiled and vetted; live capture remains unrun here.
No final freeze, refreeze, generator retirement, commit or git write was performed.

## Docs notes

The lead can integrate the standalone command into Makefile/changelog. The
[oracle README](../../tests/fixtures/goway/testdata/oracles/README.md) documents
`go run ./cmd/freeze-oracles --check --postgres` for the lead's live capture check,
and `--freeze --postgres` / `--refreeze --postgres` only at the approved freeze
barrier. Offline checks announce their PostgreSQL exclusions. Selected replay
requires complete consumption even with test/subtest filters and skip patterns.
