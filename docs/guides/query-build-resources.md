# Query build resources

The typed query API checks model, field, key, result and nullability ownership
before lowering SQL work into a private shared core in `internal/querycore`.
Hydration, model getters, codecs and lifecycle callbacks keep their concrete
types. No reflective model traversal, string dispatch or public untyped query
API replaces those checks. Typed facades keep engine state in private named
fields, preventing promotion of raw engine fields and methods.

The shared implementation owns model-independent declaration validation, active
scope selection/resolution, ordering, stable model pagination, keyset predicate
construction, model SELECT assembly/compilation, write eligibility and mutation
SQL assembly. Projection mapping, complete-record selection, join assembly and
cursor statement hashing also share implementation across model/result types. The PostgreSQL compiler
and existing AST remain the sole SQL owner.

## Consumer compatibility

Core extraction preserves query filtering, ordering, execution and mutation
signatures. Field capability reduction deliberately changes optional stored-field
computations so unused expression/aggregate families do not enter every field's
method set. Follow [typed field computations](typed-field-computations.md):

| Previous stored-field call | Replacement |
| --- | --- |
| `f.Value()` | `query.Value(f)` |
| `f.Param(v)` | `query.Param(f, v)` |
| `f.Count()` / `f.CountDistinct()` | `query.CountField(f)` / `query.CountDistinctField(f)` |
| `f.Min()` / `f.Max()` | `query.Min(f)` / `query.Max(f)` |
| `f.Sum()` / `f.Avg()` | `query.Sum(f)` / `query.Avg(f)` |

Computed expressions and aggregates retain their `Value`/`Param` methods. Query
execution `Count(ctx, db)` and row-count construction `query.Count[Model]()` are
unchanged. Update handwritten query calls with the framework pin; generated field
declarations and ownership manifests retain their format and types. No database
migration is required. Both new and existing owner/value/NULL/phase restrictions
must pass the invalid-type consumer gate.

Queries still own captured declaration/order/mapping slices. Scope callbacks stay
lazy and isolated, and execution resolves scopes from its own request context.

## Measurement conditions

Recorded results use native Go 1.27.1 on darwin/arm64 (Apple M4 Max).
Before/after results use the same SDK and consumer workloads, sequential profiles and `-mod=readonly -p=2`, with ordinary compiler optimization. Each profile
starts with its own empty Go build cache and reuses existing module downloads.
The unchanged repeat reuses that profile's cache. OS file caches, thermal state
and unrelated host load are uncontrolled; a single timing is an observation,
not a latency guarantee. The ordinary profile links and runs the existing
`productionprofile/cmd/profile`; the broad profile builds `./...` in the independent
consumer module. Neither build executes the full test suite or uses race mode.

The repository's native measurement `Runner` retains commands, wall time and RSS.
Cache totals include all regular cache files; archive totals count exported
package archives once per listed import. Task-owned cold caches are removed only
after commands finish and their evidence is retained. Ordinary shared caches are
never cleared for these measurements.

[Recorded native evidence](../evidence/query-build-resources-20261019.json)
retains exact commands, cache/archive bytes, RSS, source fingerprints and results.
The baseline includes the pre-existing uncommitted ORM/TypeScript work, unchanged
through this refactor. The consumer workload is the same, with only the required
stored-field call migration in the final variant.

| Workload | Original | Final engine + field capabilities |
| --- | ---: | ---: |
| Full consumer cold build | 162.82 s | 129.23 s |
| Full consumer unchanged build | 6.31 s | 6.01 s |
| Full consumer build cache | 9.94 GiB | 8.65 GiB |
| Ordinary cold build | 6.31 s | 5.82 s |
| Ordinary unchanged build | 0.24 s | 0.24 s |
| Ordinary build cache | 192.93 MiB | 187.49 MiB |
| Ordinary consumer archive | 46.04 MiB | 39.30 MiB |

The combined final full workload retains **13.0% less cache**, about 1.29 GiB
saved per measured cold build. The ordinary workload's cache shrinks by 2.8%.
Cold timings are single observations and do not establish a universal speedup.

These totals include the existing field-capability reduction from `43a10d9`.
It accounts for most of the cold archive improvement; extracting shared planning
inside the public package alone barely changed storage. Compared with a baseline
already containing that field reduction, the final engine uses **3.1% less cold
cache** (8.93 → 8.65 GiB). The main additional benefit is avoiding repeated
consumer compilation after an engine implementation edit.

## Cache growth across engine edits

A second controlled comparison applies the existing `43a10d9` field-capability
optimization to the captured baseline, excluding unrelated geographic additions.
Both sides use the migrated consumer calls. Each starts with an empty owned
cache, builds the full consumer, then changes only the private invalid-table
diagnostic text without adding lines or changing valid-query behavior. Go overlays
preserve virtual source paths and leave the working tree untouched.

| Full consumer workload | Field reduction only | Final shared engine |
| --- | ---: | ---: |
| Initial cold build | 133.53 s | 129.23 s |
| Initial build cache | 8.93 GiB | 8.65 GiB |
| Build after private diagnostic edit | 138.53 s | 3.71 s |
| Additional retained cache | 8.42 GiB | 6.33 MiB |
| Go compiler invocations after edit | 159 | 2 |

The final edit rebuilds only `internal/querycore` and `database/query`; consumer
compilation is reused. Additional retained cache falls by **99.93%** in this
case. Keeping the raw implementation in its own non-generic package is essential:
the intermediate same-package core still added 5.41 GiB after the same edit.

This is evidence for a private implementation edit whose typed declarations stay
unchanged. Changes to public generic methods, model declarations, exported engine
data shapes, dependencies, compiler flags or SDKs can still rebuild consumers.
It does not promise a 6.33 MiB increase for every source change or build mode.

## Runtime checks

Five alternating one-second samples use precompiled baseline/final query-test
binaries with the same workload. The retained baseline binary was built from
hash-checked sources using a Go overlay and reused for the final comparison;
the overlay does not rewrite the working tree. Both variants reproduce identical
cursor scope hashes, tokens, SQL and bindings. No database or network service is
opened by these benchmarks.

| Operation | Before ns/op | Final ns/op | Before → final allocs/op |
| --- | ---: | ---: | ---: |
| Cursor planning + compilation | 9,215 | 9,167 | 172 → 167 |
| Stored-field value | 12.66 | 28.81 | 1 → 1 |
| Stored-field parameter | 20.22 | 37.66 | 1 → 1 |
| Stored-field sum | 62.90 | 79.36 | 3 → 3 |
| Two-row insert compilation | 1,687 | 1,773 | 45 → 45 |
| Two-row upsert compilation | 2,875 | 2,957 | 76 → 76 |
| Update compilation | 1,455 | 1,518 | 44 → 44 |

These are medians on one native Apple M4 Max host. Field functions add roughly
16–17 ns in this sample without adding an allocation. Tested mutation compilation
is 2.9–5.1% slower (63–86 ns) while retaining its allocation counts. Cursor
planning removes five allocations with similar observed latency. The goal of
this change is build/cache cost; these results do not claim faster SQL execution
or end-to-end database throughput. Raw samples and conditions are retained in
the evidence.

## Verification

The final source passed native `make verify` and `make test-postgres` with race
instrumentation. The full gate included independent consumers, 1,024 intended
compiler failures, real gopls probes, real TypeScript/client tests, deterministic
generation and documentation checks. The PostgreSQL runner used existing private
configuration and retained isolated namespaces. No module or generated-output
changes were required. Exact commands, source hashes, correction rounds and
manual boundary/ownership review are retained in the evidence.

## Accumulated cache storage

One build's working set differs from accumulated storage across source revisions,
compiler flags, SDKs, operating systems and race instrumentation. The default Go
cache is shared across projects; it is not a per-project storage measurement.
Batching tests bounds temporary executables, but does not impose a retained cache
quota. A smaller shared query implementation reduces each affected artifact;
it does not remove previous revisions already cached.

Keep ordinary development caches warm. Run cold measurements only when requested,
with owned temporary caches and retained reports. Finish a coherent source batch
before full acceptance, and keep expensive acceptance jobs sequential. Do not
remove shared cache files while compilers or editors are using them. Go's built-in
age-based trimming is not a disk-size quota; strict storage limits require an
explicit cache-retention policy in addition to implementation improvements.
