# Go-way final measurement preparation

Stage 11 compares the current ORM with main on the same machine and in the same
session. The earlier Stage 6 gate separately compares ChainBuild, ChainCompile
and Hydrate1000 with `35f8d45`. Historical reports are context, not either baseline.
These commands prepare or run measurements; they do not create checkouts, write
Git state, change budgets or update frozen evidence.

## Fixed conditions and interpretation

Use native macOS, the same installed Go toolchain, database placement and settings
for both sides. Provide existing clean baseline checkouts at pinned revisions.
Record the revision used for main; do not silently compare a moving branch. Keep
the branch source unchanged throughout a run. Resolve dependencies before timing.

```sh
export GOCACHE=/private/tmp/foundry-goway-codex-gocache-r14
export GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off
```

The comparison runner also fixes `GOENV=off`, `GOTOOLCHAIN=local`, `GOGC=100`, `GOMEMLIMIT=off` and empty
`GODEBUG`, builds both
benchmark binaries before sampling and starts a fresh process per sample. It
records source hashes, toolchain, CPU count, platform, command exit statuses and
load averages before and after each trial. It retains raw logs and reports median,
minimum and maximum ns/op, B/op and allocs/op for five alternating A/B trials.
Failures or missing benchmark workloads invalidate the comparison.

Run on a quiet host, without competing builds, backups or other lanes' gates.
Load average is a condition to inspect, not a portable quietness threshold. A
median within ±5% is not proof of equivalence when noise spans that interval.
Inspect ranges and repeated results; if variation approaches the gate, retain the
inconclusive result and rerun the whole paired session under stable conditions.
Investigate the limiter before claiming a speedup. Do not compare a cold side
against a warmed side or discard inconvenient samples.

## Runtime ORM against main

[The benchmark fixture](../../tests/fixtures/consumer/orm_benchmark_test.go) already
uses `models.Users.Query()`, typed columns and `orm.Compile` in this base. Main's
own fixture uses its generated model query, fields and the historical statement carrier. Keep
those fixtures in their respective trees: copying the branch fixture onto main
would change or break the baseline. Neither the branch runner nor its fixture
needs legacy model generation.

The workload is unchanged: list/filter compilation, and 1/100/1000-row reads
through direct SQL, framework SQL and the typed ORM. SQL variants compile outside
timing; typed reads construct and compile inside timing. Both sides use the same
eight persisted fields, codecs, data setup, one-connection pools and warm-read
step. `TestORMBenchmarkReadsAgree` validates complete decoded values and one ORM
SELECT. This measures reads/compilation, not writes, eager loads or saturation.

Use private PostgreSQL configuration already available to both checkouts. Never
put connection URLs in commands or evidence. The runner requires PostgreSQL;
each test retains an isolated schema, without resets or destructive cleanup.

```sh
python3 tools/goway_compare.py orm \
  --baseline /path/to/main-checkout --baseline-ref MAIN_SHA \
  --branch "$PWD" --output /private/tmp/goway-orm-plan
```

The default writes a command/source plan without running Go or PostgreSQL. Run
with `--run` and a **new** output directory on the quiet host. The equivalent
Makefile procedure is five alternating invocations, never five main samples
followed by five branch samples:

```sh
for trial in 1 2 3 4 5; do
  (cd "$MAIN_TREE" && make benchmark-orm ORM_BENCH_COUNT=1 ORM_BENCH_TIME=1s)
  (cd "$BRANCH_TREE" && make benchmark-orm ORM_BENCH_COUNT=1 ORM_BENCH_TIME=1s)
done
```

Prefer the runner because it retains the conditions and isolates binary
compilation from measurement. Main runs its own legacy fixture and generator
outputs as checked in; no regeneration is needed for this runtime comparison.

## Stage 6 microbenchmarks

Prepare a checkout at exactly `35f8d45`; the runner rejects a different baseline.
Both sides already have the goway benchmark fixture. The commands intentionally
exclude other benchmark families and do not create a detector scope.

```sh
python3 tools/goway_compare.py stage6 \
  --baseline /path/to/pre-4a-checkout --branch "$PWD" \
  --output /private/tmp/goway-stage6-plan
```

Add `--run` with a new output directory for five alternating trials. ChainBuild
retains the built query; ChainCompile checks compilation errors and retains the
statement; Hydrate1000 checks every ID, buyer and total, and exactly one SELECT
per operation. Its fake database removes network/server variability but still
runs the public compilation, scan and collection path. Review ns/op alongside
allocations. The old +18% to −5% timing swing under host load was not a regression
verdict and is not a replacement for this measurement.

## Main-versus-branch footprint

```sh
python3 tools/goway_compare.py footprint \
  --baseline /path/to/main-checkout --baseline-ref MAIN_SHA \
  --branch "$PWD" --output /private/tmp/goway-footprint-plan
```

With `--run`, this runs each tree's consumer footprint suite and the branch goway
suite, preserving their existing ceilings and retaining every probe log. Compare
common consumer probes by name and source. The old `model_draft` probe became
`model_assignments`: report that as equivalent write-input intent with different
API reachability, not identical source. The goway suite has no counterpart on
main; report it separately. Do not use a frozen historical number as fresh main
measurement. Archive T/t symbols include compiler wrappers and generic
instantiations; they are not unique source functions or executable size.

## Whole application: 70 models, seed 1

[The synthetic tool](../../tools/goway_synthetic.py) reuses the established
`synthetic.json`, `measurement/measurement.json` and summary format from the
program's `evidence/wholeapp-70-seed1` baseline. Manifest format 2 adds separate repository ownership and hashes. The measurement
report retains its existing fields and adds test time; new sources use current
typed roles. Old manifests remain readable. Keep the historical report intact.

Historical synthetic models/projections require their historical checkout's generator.
Pass `--style both --old-repo` explicitly; default generation is native only.
New sources have handwritten ORM/rows declarations and generate only the retained
enum. The seed fixes the same model graph, CRUD/read/loading handler operations
and join reports on both sides. Each synthetic model also has a SQL compilation
test. No synthetic test connects to a database.

```sh
python3 tools/goway_synthetic.py generate /private/tmp/goway-70-trial1 \
  --repo "$BRANCH_TREE" --style both --old-repo "$MAIN_TREE" \
  --models 70 --handlers 40 --reports 5 --seed 1
python3 tools/goway_synthetic.py measure /private/tmp/goway-70-trial1 \
  --cache /private/tmp/goway-70-trial1-cold-cache --order old,new
```

These cold caches are task-owned measurement artifacts, separate from the shared build cache; the
tool refuses reused caches and never deletes one. Generation uses the current shared build cache and stays
outside timing. For repeated build comparisons use fresh source/output/cache
directories, the same seed and sizes, and alternate `--order old,new` with
`--order new,old`. Retain at least five measurements per side before judging
compile-time differences. Check free disk first; 70-model archives can be large.

The cold build uses `-p=4`, `GOMAXPROCS=4` and readonly modules. It records wall
compile/build time, post-build cache bytes, per-package archive bytes/functions
and the maximum RSS of an individual compiler process. This is **not aggregate
concurrent RSS**. The subsequent uncached `go test -count=1 ./...` records
`warm_build_test_seconds`: test compilation/linking/execution with the ordinary
build cache warmed, plus post-test cache bytes. It is not cold test time or a
runtime throughput benchmark. Earlier evidence used `-p=2` and had no test phase;
compare freshly measured pairs, not those historical seconds.

For a small harness check, generate with `--models 2 --handlers 1 --reports 1`.
Run its two modules' `go test -count=1 ./...` with r14. That checks both source
families without creating cold caches or doing the 70-model run. Use `--style new`
to validate the branch alone when main is unavailable; that cannot establish a
main comparison.
