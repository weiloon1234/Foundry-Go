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

## Model and endpoint edges

Go 1.27's compiler reads every method signature and struct field type of a type an
importing package uses, and compiles every method of any generic instantiation it
reaches from them transitively in every importing package. If a model type's method
set exposes a generic query engine, or if an HTTP endpoint's method set exposes
signed or idempotent endpoint transitions, any package that merely inspects a model
field (such as `Order.TotalCents`) or registers a simple endpoint compiles the entire
transitive generic closure into its own package archive (`.a`).

### Framework design rules

Foundry-Go eliminates these closure edges across generated models and HTTP endpoints:

- **Methodless witness**: `(M).FoundryModel()` returns `query.ModelRef[M]`. `ModelRef` is
  a zero-method generic witness that statically identifies generated models without
  pulling `database/query` engine methods into the model's own method set.
- **Small model metadata**: the only other generated model methods are
  `(M).FoundryReference()`, returning `model.Reference[M, K]`, and
  `(M).FoundryIdentity()`, returning `model.Identity`. Neither reaches a query engine.
- **Package-level model declarations**: Model relation, aggregate, extension, and
  global scope descriptors live in package-level functions (`define<Model>Relations`,
  `define<Model>Aggregates`, `define<Model>Extensions`, `define<Model>GlobalScopes`)
  rather than receiver methods on the model type. Importing a model type never forces
  the compiler to instantiate relation or query generic methods.
- **Top-level endpoint transitions**: `http.Signed`, `http.Idempotent`,
  `http.IdempotentSigned`, `http.SignedAuthenticated`,
  `http.SignedOptionalAuthentication`, `http.IdempotentAuthenticated`,
  `modelbinding.Idempotent` and `modelbinding.IdempotentAuthenticated` are top-level
  functions rather than methods on the endpoint types. Declaring a plain endpoint does
  not compile the signed or idempotent generic families into the route package.

### Consumer guidance

When authoring application models, query helpers, and HTTP routes:

1. Never declare methods on model types whose signatures mention query, relation,
   scope, aggregate, or endpoint generics.
2. Keep custom query helpers, scopes, and model extensions in package functions or
   dedicated query packages rather than receiver methods on the model struct.
3. Keep HTTP endpoint transitions as top-level function calls at route registration
   time rather than chaining through receiver methods.

### Inspecting package compile footprint

To measure the compile footprint and symbol count of an individual package archive:

```bash
# Export the package archive without link-time dead-code elimination
go list -export -f '{{.Export}}' ./your/package

# Count text (function) symbols in the archive
go tool nm <export-archive> | grep ' [Tt] ' | wc -l

# Count total defined symbols in the archive
go tool nm <export-archive> | grep -v ' U ' | wc -l
```

### Regression guards and footprint impact

The framework enforces closure-free model and endpoint contracts with two automated
regression guards:

1. **Generator closure guard** (`internal/generate/closure_guard_test.go`): Walks
   every generated model type's struct fields, value method sets, and pointer method
   sets using export data, verifying that no generic types from `database/query`
   (except zero-method `ModelRef`) are reachable, and verifying negative detection
   on intentional closure leaks.
2. **Footprint budget probes** (`tests/fixtures/consumer/footprint_test.go`): Compiles
   isolated consumer probe packages with `go list -export` and asserts that function
   symbol counts, defined symbol counts, and archive sizes stay within committed
   upper-bound budgets (~1.25x headroom).

Measured on the consumer fixture models (Go 1.27.1, darwin/arm64). The baseline is
Foundry-Go dd188a7 with the same probe bodies; the compile of the `model_type` probe
fell from 850 MB to 113 MB peak RSS and from 2.6 s to 0.12 s:

| Probe case | Pre-change baseline | Post-change footprint | Reduction |
| --- | --- | --- | --- |
| Model with relations (`model_type`) | 14,910 fn, 70.9 MB | 1,159 fn, 7,267 symbols, 4.55 MB | ~93% size |
| Standalone model (`model_isolated`) | 2,546 fn, 12.2 MB | 139 fn, 1,051 symbols, 0.60 MB | ~95% size |
| Model draft (`model_draft`) | 2,612 fn, 12.4 MB | 225 fn, 1,566 symbols, 0.95 MB | ~92% size |
| Plain HTTP endpoint (`endpoint_plain`) | 3,734 fn, 16.4 MB | 1,065 fn, 7,314 symbols, 4.62 MB | ~72% size |

## Further shared write planning

Upsert policy data, index-target inference, assignment compilation, dependency
collection and correlation qualification also live in the private shared core.
INSERT SELECT and joined UPDATE/DELETE lower source/destination-checked mappings
before SQL assembly, instead of compiling that assembly for every source/model
combination. Public facades retain private named state; no raw engine fields or
methods are promoted.

Model-specific validation, literal input types, field mutators, timestamp
preparation, transaction ownership and complete decoders retain their existing
owners. Source windows, ambiguity rollback, global/tenant scopes, parameter order,
partial conflict indexes and resource bounds use the same AST/compiler rules.

This further extraction preserves public calls, generated declarations, wire
formats and database schemas. Consumers already using the preceding typed-field
API need only update their framework pin. The earlier stored-field migration
above still applies when upgrading from an older API; this extraction adds no
deprecated methods, compatibility wrappers or additional migration steps.
[Recorded continuation evidence](../evidence/query-write-build-resources-20261020.json)
retains matched source overlays, commands and structural resource results. This
baseline already includes the preceding engine/field changes and the current
consumer fixtures. Compare its matched columns, not absolute totals from the
older 2026-10-19 workload:

| Matched workload | Before | Further shared planning |
| --- | ---: | ---: |
| Ordinary build cache | 260.93 MiB | 226.57 MiB |
| Ordinary consumer package archive | 112.72 MiB | 78.17 MiB |
| Broad consumer build cache | 8.78 GiB | 8.60 GiB |
| Additional cache after private upsert diagnostic edit | 8.27 GiB | 6.54 MiB |
| Compiler invocations after that edit | 160 | 2 |

The ordinary total cache shrinks by 13.2%, its own package archive by 30.6%, and
the broad total cache by 2.0%. These totals include dependencies and ordinary
compiler artifacts; they are not accumulated shared-cache disk usage.
The diagnostic edit adds no source lines and changes no declarations, valid SQL
or bindings. Its final rebuild reuses consumer compilation.

The resumed private diagnostic edit took **119.23 s before and 4.26 s after**
with no other Go build detected during either segment. The final edit rebuilt
only `internal/querycore` and `database/query`. This isolates a private upsert
implementation edit whose typed declarations remain unchanged; it does not
promise the same benefit for model, API, dependency, flag or SDK changes.

Separate Go builds overlapped the initial comparison and the repeated baseline
cold build. Those timings are retained but do not establish a matched cold-build
speedup. Completed caches were reused to resume the unfinished segments rather
than restarting completed compilation. Structural cache/archive sizes reproduce
across the runs; only uncontended segments support the private-edit timing above.

Five alternating one-second samples from matched precompiled test binaries give:

| Planning operation | Before → after ns/op | Before → after B/op | Before → after allocs/op |
| --- | ---: | ---: | ---: |
| Upsert with expression/partial index target | 3,052 → 3,070 | 6,725 → 6,717 | 82 → 82 |
| INSERT SELECT | 2,243 → 2,187 | 5,688 → 5,264 | 60 → 59 |
| Joined UPDATE | 7,039 → 6,983 | 16,664 → 16,232 | 162 → 161 |

These are local medians, not end-to-end database throughput claims. Upsert
allocation count is unchanged; each tested source-write case removes one allocation.
Both variants pass the same SQL/binding, scope, index, mutator, timestamp and
rejection contracts. Final native `make verify` and required PostgreSQL/race gates
passed on the measured, unchanged source, including independent consumers, 1,026
compiler rejections, real gopls/TypeScript clients and deterministic generation.
The 2026-10-19 tables below remain historical evidence for the earlier extraction.

## Shared runtime metadata

Record-field codec metadata and codec-only callbacks do not depend on the model
that owns a field. The private runtime separates that metadata from concrete
model getters. Model/projection declarations, keys, mutation input types and
result decoders keep their public contracts and existing validation ownership.

HTTP endpoints share type-independent transport preflight and response
publication. Typed decoding, lifecycle stages and handler execution keep their
existing order and isolation boundaries. Body and response cleanup remain under
the original request owner. JSON parsing/schema checks and validation execution
envelopes share private implementation while preserving concrete DTO decoding,
all-or-zero failure results and the validation fast path.

Datatable declaration descriptions and structural request validation share one
private metadata owner. The complete filter tree is checked before scalar
callbacks execute. SQL predicate composition, null-aware complements, typed sort
expressions, row exports, current authorization and source scoping retain their
existing owners. This does not introduce a request instruction interpreter.

These internal changes require no handwritten consumer migration, regeneration,
wire change or database migration. They add no deprecated functions. The earlier
stored-field computation migration above still applies to older framework pins.

A frozen Boss Club source snapshot compares `e9fcb1a` with this continuation,
using one fixed private framework replacement path. The actual consumer source
and framework pin were not modified. This is an unpublished local-source
comparison, rather than verification of a published upgrade. The historical
workloads elsewhere in this guide are separate comparisons.

| Boss Club workload | Before | Shared runtime metadata |
| --- | ---: | ---: |
| Cold ordinary build, median of five | 285.59 s | 282.19 s |
| Unchanged ordinary build, median of five | 37.44 s | 36.50 s |
| Ordinary build cache | 9.323 GiB | 9.259 GiB |
| Build + vet cache | 9.390 GiB | 9.325 GiB |
| Build + vet + race compilation cache | 21.356 GiB | 21.208 GiB |

Ordinary cache falls by **0.68%**. The complete retained profile saves
**151.65 MiB, or 0.69%**. Ordinary cache byte counts reproduce exactly across
five fresh task-owned caches per side. Cold timing ranges are 284.69–287.22 s
before and 281.33–284.13 s after. The local median improves by **1.19%**;
this is a small improvement under the measured configuration.

Unchanged-build ranges overlap at 35.97–38.15 s before and 35.74–37.65 s after,
so they do not establish a repeatable speedup. Each unchanged ordinary build
compiles zero packages, links the CLI once and adds zero retained cache bytes.
The retained trace therefore attributes the warm delay to linking.

Five alternating one-second samples from matched precompiled test binaries give
the following runtime medians. These probes execute no database work.

| Operation | Before → after ns/op | Before → after allocs/op |
| --- | ---: | ---: |
| Repeated model metadata validation | 209.0 → 221.8 | 2 → 2 |
| Shallow table request preparation | 228.8 → 213.0 | 12 → 12 |
| Nested table request preparation | 1,572 → 1,509 | 88 → 88 |
| JSON DTO decoding | 3,491 → 3,516 | 43 → 43 |
| Validation rule check | 399.5 → 396.6 | 1 → 1 |
| 100-item JSON endpoint through router | 170,635 → 167,994 | 2,733 → 2,733 |
| Same endpoint through kernel | 170,639 → 169,897 | 2,754 → 2,754 |

Repeated metadata validation costs about 13 ns more, or 6.1%, in this probe.
Table preparation improves by about 4–7%; DTO, rule and endpoint timing ranges
overlap. Allocation counts are unchanged in all seven paths. Allocated bytes are
unchanged outside the endpoint cases, whose small variation is retained in the
evidence. These results cover the exercised operations, not registration costs
or end-to-end database throughput.

The largest three consumer archives contain about 147 MiB of stored machine
code within about 2.6 GiB of archives. Symbol names, references, other data and
debugging information account for most of their storage. This suggests that
retained type and method graphs limit further savings from shared helper bodies.
These object categories are descriptive, not independently removable costs.

Commands use native Go 1.27.1 on darwin/arm64, `-mod=readonly -p=2`,
`GOMAXPROCS=2` and `-buildvcs=false`. Before/after trials alternate. OS file
caches, thermal state and unrelated host load remain uncontrolled. Race profiling
compiles and links with `-exec=/usr/bin/true`; it executes no application tests
or database code. Its one cold observation per side does not establish a race
compilation speedup. Cache-plus-temporary-file peaks were sampled every five
seconds at about 35 GiB during race compilation; brief higher peaks may be missed.

[Recorded runtime extraction evidence](../evidence/typed-runtime-cost-20261020.json)
retains source fingerprints, commands, timing samples, cache/object breakdowns
and verification. Task-owned measurement caches are removed after child exit.
Shared and other-agent caches are not deleted by this comparison.

The measured Go/module source passed native `make verify` and focused races for
query, HTTP, contract, validation and datatable packages. All 3,060 exported
declarations in the affected packages and 285 generated Go files match the
baseline. PostgreSQL checks used existing private configuration and retained
isolated test namespaces. Registration allocation neutrality was not measured.

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
