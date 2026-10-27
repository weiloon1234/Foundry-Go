# Containment performance repair

Base: `9c2a8309`, detached worktree. No commits are created by this task.

## Result

The measurements below describe the performance slice before the P2 follow-up.
No refreshed benchmark result is claimed for the checked conversion added below.

Runtime compilation and all nine PostgreSQL read cases use fewer allocations and
have lower medians than main `b8816902`. ChainCompile is 1.3% above the paired
Stage 6 baseline; Hydrate1000 is 9.6% faster with 109 rather than 5,065 allocations.
**ChainBuild does not meet its no-slower target:** 150.7 ns versus 145.3 ns (+3.7%),
although allocations fall from six to four. Its ranges overlap; this is a remaining
acceptance gap, not proof that containment makes the target impossible. No guarantee
or test was relaxed to claim a pass. Results faster than the lower end of the ±5%
band are reported as improvements, rather than described as literal parity.

[Measurement data](goway-containment-performance.json) records all medians/ranges,
five samples per side, conditions and source hashes. Full raw reports, logs,
binaries and profiles remain under the task paths listed there.

## Design and ownership

| Owner | Change |
| --- | --- |
| `database/codec/parameter_type.go:38`, `database/codec/codec.go:58` | Only the private built-in constructor issues the trust capability. Public New, Validated and Adapt retain isolation. Nullable/JSON delegate to their contained owners. Binding-result validation is nongeneric and shared. |
| `internal/sqlcallback/codec.go:13` | Decode has one captured result on the application path and no capture on the trusted path; a failed decode returns zero. The private concrete scanner has a shared native view that seals causes before SQL can inspect them. Exact type assertion rejects embedded overrides. |
| `internal/sqlcallback/value.go:63`, `database/orm/query.go:275` | Reuse owned destination adapters and a scratch model/scanner set for one collection. Zero scratch before every row, copy completed values, discard the entire result on failure, clear retained destinations on closure. |
| `database/row_collection.go:17`, `internal/sqlcallback/row_batch.go:46` | Reuse the collection's isolated invocation for sequential driver Next/NextRow. Hold the stream mutex throughout collection and cleanup. Strip the private lease before application drivers receive context, so nested queries cannot borrow it. Keep individual native ScanColumn/Close isolation: foreign scan/close frames need to regain control to release their locks. |
| `internal/sqlcallback/driver.go:34` | Success/sentinel returns avoid the captured state needed only for bounded retry inspection. Driver failures retain sealed causes and existing retry/outcome classification. |
| `internal/ormcore/raw.go:39` | Raw hydration delegates to the collection's borrowed Row, preserving column remapping and encryption scope without reacquiring the stream mutex. |
| `internal/callback/invoke.go:30,46` | Construct Goexit diagnostics only on failure; use the same returned-error policy for trusted failure paths. Wait for actual callback exit, including deferred cleanup. |
| `internal/querycore/comparison_binding.go:26` | One isolated invocation for a bounded raw membership list; trusted codec binders retain their own application boundary. Preserve in-order validation precedence and publish no partial bindings. |
| `internal/querycore/predicate_state.go:20`, `database/orm/column.go:130` | Co-locate private retained-literal/predicate holders, inline the common single value, cache immutable operands and codec binders in declarations, and pack comparison metadata. Values stay behind the formatting boundary. |

`Rows.FoundryCollect` is an exported bridge between framework packages using the
existing internal CollectionSeal. It adds no application-issued capability or
configuration switch. Its borrowed Row cannot be retained/shared by framework
decoders. Collection retains existing query/context bounds and actual callback
ownership; no new worker queue or unbounded background task is introduced.
There are no dependency or generated-contract changes.

The scanner constructor deliberately is not inlined: archive symbols showed 76
copies of its generic closure plus 24 inline wrappers. Sharing that constructor
removes 82 reachable copies. All **148 native ORM footprint probes** pass their
unchanged ceilings; the tight `aggregate_families` probe is **4,352 / 4,369**
functions (19,258,928 archive bytes). Both consumer footprint suites also pass.
These are compiler/archive symbol counts, not unique source functions or executable
sizes.

## Measurements

Native macOS / Apple M4 Max, 16 logical CPUs, Go 1.27.1. The unchanged Stage 11
runner uses five alternating A/B processes, 1-second benchmark windows, matching
GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off, GOGC=100 and local toolchain settings.
Task-owned GOCACHE is `/private/tmp/foundry-perf-gocache`. PostgreSQL uses the
existing private configuration with required mode; no URL is retained in evidence.
Other host applications were active; load and dispersion remain in the JSON.
Profiles ran separately and are not included in reported benchmark samples.

“Before” is this worktree before edits. “Baseline” is the paired reference from
the final comparison, not a frozen historical number. The before sessions have
their own paired baselines in the JSON. Times below are medians; each cell is
**time in µs / allocations per operation**.

### Stage 6 against `35f8d45`

| Workload | Before | Baseline | After | After vs baseline |
| --- | ---: | ---: | ---: | ---: |
| ChainBuild | 0.1971 / 8 | 0.1453 / 6 | 0.1507 / 4 | +3.7% |
| ChainCompile | 4.5820 / 83 | 1.8220 / 56 | 1.8450 / 52 | +1.3% |
| Hydrate1000 | 4,182.9620 / 94,135 | 195.6200 / 5,065 | 176.8580 / 109 | -9.6% |

ChainBuild ranges: baseline 140.1–184.8 ns, after 147.6–187.6 ns. ChainCompile:
1.775–1.889 µs versus 1.753–1.921 µs. Hydrate1000: 187.406–201.656 µs versus
174.189–189.545 µs. Hydration checks every field of all 1,000 rows and exactly
one query per timed operation. ChainCompile checks errors and retains its result;
ChainBuild retains the complete query.

### Runtime against main `b8816902`

| Workload | Before | Main baseline | After | After vs main |
| --- | ---: | ---: | ---: | ---: |
| compile/filters | 17.1260 / 239 | 12.3640 / 228 | 4.2980 / 99 | -65.2% |
| compile/list | 9.9260 / 138 | 9.6050 / 133 | 4.5150 / 69 | -53.0% |
| read/1/direct-sql | 109.4700 / 468 | 109.2030 / 466 | 96.8540 / 224 | -11.3% |
| read/1/framework-sql | 144.9960 / 801 | 142.5210 / 799 | 107.9360 / 344 | -24.3% |
| read/1/typed-orm | 160.4530 / 958 | 160.1790 / 947 | 115.8930 / 417 | -27.6% |
| read/100/direct-sql | 2,806.5850 / 28,078 | 2,744.4790 / 27,780 | 1,777.5240 / 8,950 | -35.2% |
| read/100/framework-sql | 4,721.8290 / 50,394 | 4,602.2990 / 50,097 | 2,275.3950 / 14,417 | -50.6% |
| read/100/typed-orm | 4,798.0870 / 50,551 | 4,662.4120 / 50,245 | 2,148.8430 / 12,114 | -53.9% |
| read/1000/direct-sql | 25,062.9180 / 279,514 | 23,966.0260 / 276,522 | 14,797.1940 / 88,907 | -38.3% |
| read/1000/framework-sql | 43,362.5870 / 501,652 | 42,973.7320 / 498,665 | 18,890.9510 / 142,973 | -56.0% |
| read/1000/typed-orm | 43,365.8480 / 501,809 | 42,538.0600 / 498,813 | 18,045.8070 / 119,071 | -57.6% |

Each runtime process also runs TestORMBenchmarkReadsAgree. Read benchmarks validate
returned records and issue one SELECT per operation. Direct SQL, framework SQL and
typed ORM use the same prepared dataset and connection conditions on both sides.
All nine measured read cases have fewer, rather than extra, allocations relative to main.

## Experiments and invariant proof

The first fast-codec iteration left Hydrate1000 at 576 µs / 7,105 allocations.
Separate allocation/CPU profiles identified per-row driver isolation and destination
slice copies as the remaining costs. Collection-owned Next containment and scratch
reuse remove those costs. Profiles are retained at
`/private/tmp/foundry-perf-hydrate.{cpu,mem}` and
`/private/tmp/foundry-perf-chain-build.{cpu,mem}`. macOS CPU samples included substantial
runtime kevent/GC accounting, so no precise CPU percentage is claimed for the small
remaining ChainBuild gap.

An additional five-pair experiment passed column metadata by reference. ChainBuild
remained 150.5 ns against 146.7 ns, with the same four allocations; it did not justify
the mechanical API-internal changes, which were removed. Its report is retained at
`/private/tmp/foundry-perf-after-stage6-ref2/comparison.json`. The restored Go source fingerprint at completion of that slice matched the
completed runtime comparison. The subsequent P2 adapter correction below has not
been remeasured.

| Guarantee | Regression source |
| --- | --- |
| Only privately issued codecs skip isolation; public parameter metadata cannot grant trust; built-in Decode allocates zero | [trusted_test.go](../../database/codec/trusted_test.go), TestOnlyPrivateCodecCapabilitySkipsIsolation |
| Fast and isolated failures keep exact messages, classification and identity | [trusted_test.go](../../database/codec/trusted_test.go), TestTrustedFailureKeepsIsolatedDiagnostic |
| Native errors are opaque, then regain identity; hostile Error/Format never runs; embedded/nil-embedded scanner overrides stay isolated | [codec_test.go](../../internal/sqlcallback/codec_test.go) |
| Trusted JSON codecs still contain application panic/Goexit | [json_test.go](../../database/codec/json_test.go) |
| No partial comparison bindings; earlier validation wins; overriding Bind cannot inherit trust; retained literals remain opaque and independent | [comparison_batch_test.go](../../internal/querycore/comparison_batch_test.go) |
| Failure on the second decoded row discards the complete result, closes rows, retains identity/classification and permits a healthy read afterward | [read_containment_test.go](../../database/orm/read_containment_test.go) |
| Legacy Next/native NextRow panic/Goexit and native ScanColumn Goexit release SQL locks and pool slots; autocommit failures stay Unknown/CommitUnknown | [driver_batch_test.go](../../database/orm/driver_batch_test.go) |
| Driver context cannot retain the private batch lease; deferred application cleanup keeps the batch owned until actual exit | [row_batch_test.go](../../internal/sqlcallback/row_batch_test.go) |

Existing tests are not weakened. Joined-plan binding instrumentation now counts at
the public codec callback, because private compiler-owned binding replaced its old
raw Bind slot. All original SQL/count/budget assertions remain. The raw-row lock
regression found during races was fixed at the adapter and its existing scope test
passes unchanged. Repeated failure tests assert zero active callbacks and subsequent
healthy operation, rather than relying on noisy process-wide goroutine counts.

## Acceptance

Commands use `GOCACHE=/private/tmp/foundry-perf-gocache GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.
PostgreSQL commands source `/Users/weiloon/Projects/rust-go/Foundry-Go/.env.test`
with shell export enabled and set `FOUNDRY_TEST_POSTGRES_REQUIRED=1`; credentials
are not printed. Logs are retained under `/private/tmp/foundry-perf-*.log`.

| Command | Result | Log suffix |
| --- | --- | --- |
| `go build ./...` | PASS | `build` |
| `go vet ./...` | PASS | `vet` |
| `make test` | PASS, including `internal/retirement` | `test` |
| `go test -race -timeout=10m ./internal/callback ./internal/sqlcallback ./database/codec ./internal/querycore ./database ./database/orm ./internal/ormcore` | PASS, including required PostgreSQL | `final-focused-race` |
| `make race` | PASS | `race` |
| `make fixture-check` | PASS | `fixture` |
| `make generate-check` | PASS | `generate` |
| `make docs-check` | PASS | `docs` |
| `make fmt-check`, `git diff --check` | PASS | `fmt`, `diff-check` |

`make race` uses the Makefile's six-package batching and 20-minute per-batch
timeout. It covers all root and fixture packages, including every H1 containment
package and native ORM packages under `database/orm/...` (there is no root `orm`
package). No assertions or budget ceilings are relaxed. Optional external editor/TypeScript/Redis
services are not part of this brief's required-mode gate.

Final comparison commands (all exit 0, five pairs for timing):

```sh
python3 tools/goway_compare.py footprint --baseline /Users/weiloon/Projects/rust-go/Foundry-Go-perf-main-baseline --baseline-ref b8816902 --branch "$PWD" --output /private/tmp/foundry-perf-footprint-final --run
python3 tools/goway_compare.py stage6 --baseline /Users/weiloon/Projects/rust-go/Foundry-Go-perf-stage6-baseline --branch "$PWD" --output /private/tmp/foundry-perf-after-stage6-final --run
python3 tools/goway_compare.py orm --baseline /Users/weiloon/Projects/rust-go/Foundry-Go-perf-main-baseline --baseline-ref b8816902 --branch "$PWD" --output /private/tmp/foundry-perf-after-orm-final --run
```

Iteration failures were retained and corrected before these final results: an
unused import/new-test identifier error, stale joined-plan instrumentation, the
raw-row reentrant-lock defect, and excess generic footprint. The reference-passing
experiment initially missed several private callers; the corrected experiment
completed, did not improve the result, and was removed. None of those intermediate
sources is represented as final acceptance.

All final gate commands exited 0. Before/after source hashes are retained in the
JSON; Go sources at completion of the performance slice matched the completed runtime comparison exactly. A final
documentation check follows this evidence update.

After the last Go command, cleanup removes only the task-owned
`/private/tmp/foundry-perf-gocache` and the two clean temporary baseline worktrees
named above. Raw benchmark reports/logs/binaries/profiles remain available.
The cleanup result is recorded in `/private/tmp/foundry-perf-cleanup.log`.
The unrelated pre-existing caches are untouched. Edits remain uncommitted.


## Security review P2: cached comparison conversion

The cached column binder now uses a checked generic assertion. A nil interface is
passed as V's zero value only when V is an interface type; the declared codec
continues to own NULL conversion. Other mismatches return the existing owner-issued
Panicked fault and exact `panicked: query comparison binding panicked` diagnostic.
The normal typed path adds no callback invocation or reflection; reflection is
limited to the nil/assertion-failure branch.

Audit: equality/inequality and membership all share columnWithCodec.literalBind,
including lookup/default consumers of Comparison.bindValue. No other cached direct
adapter introduced by this slice contains an unchecked generic type assertion.
The existing expr row/scalar adapters still use Comparison.Bind and remain inside
the compiler's isolated callback path; numeric bound assertions are constrained to
non-interface NumberValue types and are not part of the new cached path. Scanner
adaptation uses an exact private type assertion and typed decode results.

Public Compile regressions in database/orm/comparison_adapter_test.go cover nil
interface equality/inequality and membership, codec-owned SQL NULL rejection, and
mismatched nil/non-nil retained values. They check no partial SQL/arguments and
exact fault classification/diagnostic. This correction does not claim refreshed
performance numbers; the benchmark results above identify the earlier measured
source hashes.

Follow-up verification uses `GOCACHE=/private/tmp/foundry-perf-gocache`,
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`, with the existing PostgreSQL configuration
sourced privately and `FOUNDRY_TEST_POSTGRES_REQUIRED=1`. The cache was recreated
at the requested path after the earlier measurement cleanup.

| Command | Result |
| --- | --- |
| `go test -race -timeout=20m ./database/orm/... ./internal/querycore` | Segmented PASS: all existing cases and the new equality/NULL/mismatch cases passed; two new membership assertions initially expected individual parameters instead of the existing packed array parameter. Their expectations were corrected without runtime changes. |
| `go test -race -timeout=20m ./database/orm -run '^TestCompileInterfaceComparisonNilValuesReachCodec/(membership\|not_membership)$'` | PASS for both corrected membership cases. |
| `make test` | PASS, including root ORM regressions and retirement guard. |
| `make docs-check`, `make fmt-check`, `git diff --check` | PASS. |

Logs: `/private/tmp/foundry-perf-p2-race.log`, `foundry-perf-p2-race-rerun.log`,
`foundry-perf-p2-test.log`, `foundry-perf-p2-docs.log` and
`foundry-perf-p2-fmt.log`. The final evidence-only edit receives another docs check.
No commits were created.
