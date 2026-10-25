# Frozen legacy oracles

This directory is **pending**. No final freeze has been run. Live old/new
comparisons remain enabled, and ordinary tests never write expectations.

From the repository root, the lead can run these without a Makefile change:

```sh
cd tests/fixtures/goway
GOCACHE=/private/tmp/foundry-goway-codex-gocache-r13 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off go run ./cmd/freeze-oracles --check
# Lead-only PostgreSQL capture/replay, using the existing private test configuration:
GOCACHE=/private/tmp/foundry-goway-codex-gocache-r13 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off go run ./cmd/freeze-oracles --check --postgres
# Only at the integrated-tree freeze barrier:
GOCACHE=/private/tmp/foundry-goway-codex-gocache-r13 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off go run ./cmd/freeze-oracles --freeze --postgres
# An intentional later replacement requires this distinct command:
GOCACHE=/private/tmp/foundry-goway-codex-gocache-r13 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off go run ./cmd/freeze-oracles --refreeze --postgres
```

`--check` runs real golden paths and measurements into temporary storage, checks
coverage and provenance, replays the staged frozen records in a fresh test process,
then removes that temporary storage. It does not freeze.
`--freeze` accepts only pending state; `--refreeze` accepts only valid frozen state.
Both publish only after all live comparisons and measurements pass. A lock excludes
concurrent recorders. Do not edit sources during recording: a before/after source
hash check rejects a changing tree. The default temporary check excludes PostgreSQL
comparisons explicitly. Publication requires `--postgres`, which enables required
PostgreSQL mode: a missing backend fails rather than skipping observations. No
generator runs are involved.

`records.json` contains observations keyed by test/subtest, stable assertion site
and occurrence. Transcripts preserve the existing golden formatting of SQL,
typed binding values, hydrated results, cursor tokens and callback traces. Compile
cases have no hydrated rows; write/count paths record the evidence their existing
workload produces. Relation hydration adds public loaded-slot values and states.
Existing fixture normalization of generated UUIDs, encrypted envelopes and password
binding shapes stays in force; no production credentials or plaintext secrets are
recorded. The PostgreSQL comparison uses fixed fixture seed IDs in a fresh isolated
schema so repeated real legacy reads have stable identities; no returned model or
aggregate expectation is synthesized. PostgreSQL capture stays with the lead.

Each observation carries commit, actual command, Go toolchain/platform and a digest
of the shared source-hash manifest. That manifest hashes the actual checkout's Go
sources, module files and ownership manifests, including uncommitted sources.
The commit alone does not assert a clean checkout. Footprints record actual archive
T/t symbols, defined symbols and bytes: every oldmodels package, oldreporting,
extension_slots_old and consumer model_type/model_isolated/model_draft. The symbol
parser is shared with the ordinary footprint gate. Counts are historical evidence,
not new ceilings, timing benchmarks or measurements on another platform.

`state.json` is the publication receipt: its digest covers records and provenance.
The integrity test rejects changed/missing records, missing provenance, unsupported
state, or unreceipted data in pending state. The explicit command owns the receipt;
this is a review/integrity guard, not authentication against a person deliberately
rewriting both evidence and receipt. Publication replaces data then receipt; an
interruption between them fails closed and needs review, never automatic acceptance.

After publication, new-API results compare to the frozen legacy observations as well
as the live legacy results. Until retirement the latter still run. At 8c deletion,
remove only legacy execution and feed the same current transcript and stable ID
into the frozen comparator; do not regenerate expectations from the ORM side.
Some tests intentionally share the old concrete model type: port those declarations
and normalize any owner-qualified representation explicitly during that separate
retirement, preserving cursor wire identity. The integrity protocol does not delete
oldmodels, oldreporting, generated files, tests or historical evidence.

After retirement, re-freezing requires the approved legacy source checkout. Missing
legacy packages fail the command; it never substitutes ORM output as the oracle.

Review hardening: `tests/oracle` is the shared owner used by both fixture modules.
`comparisons.json` is the reviewed source census, independent of site discovery.
It covers all test sources in goway and consumer (excluding compilefail/testdata),
and names the paired legacy/new test entry points. Sources with no paired entries
are ordinary behavior, integration, compiler or harness tests, not excluded golden
comparisons. Before a freeze, review source changes and update this census; capture
fails if its hashes or paired entry points differ. Cross-file assertion helpers are
followed. The census is a capture-time retirement checklist, not a requirement to
keep deleted legacy source after the freeze.

Snapshot nodes preserve kind, field names, nil/empty, pointer presence, strings
(including invalid UTF-8) and sequence boundaries. Model type identities and
pointer addresses are normalized. Model-only field filtering stops at model
boundaries. Scalar internals (including typed IDs, decimals and timestamps) remain
lossless; loaded relations re-enter the model boundary for their models and pivots. Sensitive randomized values retain the existing
fixture's type/shape and input-equality observations, not ciphertext or fresh hashes.

Every package's TestMain enforces all its golden IDs on unfiltered replay. The
recorder explicitly requests full replay for its reviewed test selection. Normal
`-run` and `-skip` selections determine expected records from the frozen inventory,
independently of consumed observations. Selected parent bodies and every occurrence
in selected subtests are required, even when they emit zero observations. Excluded
siblings and skipped descendants are not required; `-list` is inspection only.
Package identity is explicit so the root goway package cannot consume integrations'
records. After a full PostgreSQL freeze, an unfiltered full replay requires that
backend; select the unit tests explicitly for an offline replay. Footprints are
remeasured by the recorder, not ordinary fixture test processes.

Capture (including `--refreeze`) reads a fresh pending state in a temporary directory.
It never checks the published inventory during capture. The new bundle is validated,
then replayed in fresh test processes, then published only on explicit freeze/refreeze.
`--check` additionally requires the published observations to match if a freeze exists.

The command enforces GOFLAGS=-p=4, GOWORK=off, GOENV=off and GOTOOLCHAIN=local;
nonempty alternative GOFLAGS or workspace paths are rejected. Compiler flags,
overlays and modfile overrides are thereby disabled. Architecture/tool overrides
are removed from subprocess environments; the host target is fixed. Each record
contains effective go-env build settings, empty gcflags, actual working directory,
command, toolchain and source manifest. Footprints in the consumer module record
that module's directory and go.mod, not the recorder's directory.

Default replay lookup supports the checkout layout and the release tool’s sibling
`consumers/full`, `consumers/ordinary` and `consumers/goway` layout. An explicit
FOUNDRY_ORACLE_READ still selects the temporary capture/replay directory.

Consumer replay reads a sibling module, so use make (which supplies the shared
source fingerprint) or `go test -count=1` for direct replay, including filtered runs.
The shared assertion calls `testkit.TrackExternalInputs`; the recorder always uses
`-count=1`. This prevents cached consumer results from hiding changed oracle inputs.
