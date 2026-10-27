# Stage 11 paired measurements

**Result: performance/resource targets are not all met.** Runtime filter compilation, read allocations, model-package footprint, 70-model compiler RSS and all three Stage 6 microbenchmarks miss their targets. No source fix or budget change was made.

Measured on 2026-10-27, native macOS / Apple M4 Max, 16 logical CPUs, 64 GiB RAM, `go1.27.1`. Main: `b8816902`; branch: `9c2a8309`; Stage 6 baseline: `35f8d45`. See the [raw data](goway-stage11-measurements.json) and [runbook](../guides/goway-final-measurements.md).

## Conditions and interpretation

- Same machine/session, installed Go toolchain and PostgreSQL settings; one connection per benchmark pool, warm reads, eight persisted fields and full decoded-value/single-SELECT agreement checks. No parallel heavy measurement jobs.
- `GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`, `GOENV=off`, `GOTOOLCHAIN=local`, `GOGC=100`, `GOMEMLIMIT=off`, empty `GODEBUG`. Synthetic builds additionally use readonly modules and the prescribed `-toolexec` RSS wrapper. The synthetic helper uses Go defaults for GC/memory/debug settings after filtering its environment; these equal the stated values.
- Runtime and Stage 6: five adjacent A/B trials per session, fresh process per sample, `1s`, `-benchmem`. First sessions showed ranges approaching/exceeding 5%, so the entire paired session was repeated. Both sessions and every sample are retained. **Session 2 is primary; no pooled or trimmed result is substituted.**
- Synthetic: five cold paired trials, 70 models / 40 handlers / 5 reports, seed 1; orders `old,new`, `new,old`, `old,new`, `new,old`, `old,new`. Generation/dependency resolution and binary construction are outside runtime timing. Cold app builds use separate fresh caches per side/trial.
- Load averages are recorded before/after every benchmark trial and synthetic style. Background GUI/indexer activity was observed; compile-phase load includes this run’s compiler work. Several PostgreSQL timing ranges remain noisy after repetition, so their within-5% medians do not prove equivalence.
- Disk was checked before heavy runs, with a conservative 40 GiB headroom reserve. Observed use stayed at or below 76%, below the 85% stop limit. All eleven task cache roots, the incidental Python bytecode file and both temporary baseline worktrees were removed after recording. Raw logs/profiles/generated app inputs remain in the raw-artifacts directory recorded in JSON.
- The harness marks branch dirty because its Python import created an untracked `tools/__pycache__/goway_synthetic.cpython-314.pyc`; tracked files and measured Go/module/harness hashes stayed unchanged. All three trees were clean again before cleanup/evidence writing.
- Values below retain raw units. Ranges are `[min, max]`, not confidence intervals. Timing percentages are changes in medians. Archive T/t counts include wrappers and generic instantiations, not unique source functions or executable size.

## PLAN targets

| Area | Result | Evidence |
| --- | --- | --- |
| model-reading package | PASS | <=1158 functions |
| one-query package | PASS | <=30% of PLAN reference 4741 functions |
| models fixture | FAIL | functions and bytes <=25% of fresh main |
| models synthetic | FAIL | functions and bytes <=25% of fresh main |
| synthetic compile RSS | FAIL | <=5000000000 bytes (5 GB) |
| per-model cost budgets | PASS | existing per-kind and slope ceilings unchanged |
| whole app compile/cache/archive/test | PASS (reporting target) | report fresh main-versus-branch measurements |
| ORM runtime | FAIL | within +/-5% of main |
| read allocations | FAIL | 0 extra allocations |
| Stage 6 microbenchmarks | FAIL | ChainBuild <= pre-4a; compile and hydrate within +/-5%; no extra allocations |
| handler package growth | PASS | median <=9820 functions and <=38000000 bytes |
| eager loading / N+1 | NOT RERUN (outside measurement-only scope) | Not part of this measurement turn |
| SQL golden suite | NOT RERUN (outside measurement-only scope) | Not part of this measurement turn |
| typing catalogue | NOT RERUN (outside measurement-only scope) | Not part of this measurement turn |
| Go to TS contracts | NOT RERUN (outside measurement-only scope) | Not part of this measurement turn |
| editor probes | NOT RERUN (outside measurement-only scope) | Not part of this measurement turn |
| make verify and PostgreSQL races | NOT RERUN (outside measurement-only scope) | Not part of this measurement turn |

## Runtime ORM vs main

### Session 1 — retained noisy first session

| Workload | Main ns/op median [range] | Branch ns/op median [range] | Δ % | Median B/op | Median allocs/op |
| --- | --- | --- | --- | --- | --- |
| Compile/filters | 12223 [12112, 12353] | 17002 [16851, 17161] | 39.09842100957213 | 12357 → 12804 | 228 → 239 |
| Compile/list | 9503 [9414, 11819] | 9865 [9853, 9875] | 3.809323371566875 | 7259 → 7498 | 133 → 138 |
| PostgresReads/1/direct-sql | 108717 [107456, 108996] | 110132 [105923, 115069] | 1.301544376684416 | 24271 → 24535 | 466 → 468 |
| PostgresReads/1/framework-sql | 142674 [139001, 144980] | 143128 [139775, 193098] | 0.3182079425823936 | 40221 → 40490 | 799 → 801 |
| PostgresReads/1/typed-orm | 160999 [154513, 161989] | 155693 [155419, 159255] | -3.2956726439294703 | 48155 → 48924 | 947 → 958 |
| PostgresReads/100/direct-sql | 2690070 [2679831, 2768211] | 2741840 [2727414, 2769176] | 1.9244852364436627 | 1609054 → 1636502 | 27781 → 28078 |
| PostgresReads/100/framework-sql | 4593839 [4549285, 4725458] | 4671539 [4654214, 4770314] | 1.691395802073159 | 2664778 → 2692520 | 50097 → 50394 |
| PostgresReads/100/typed-orm | 4642712 [4550042, 5862331] | 4703590 [4684026, 4735166] | 1.3112594535263034 | 2672682 → 2700927 | 50245 → 50551 |
| PostgresReads/1000/direct-sql | 24054921 [23921395, 25264671] | 24657077 [24393645, 25026617] | 2.503254947293332 | 16322599 → 16601584 | 276523 → 279515 |
| PostgresReads/1000/framework-sql | 42135579 [41801105, 42399336] | 42601295 [42390168, 46309010] | 1.1052796972363943 | 26827845 → 27106377 | 498663 → 501653 |
| PostgresReads/1000/typed-orm | 42316684 [41787998, 42827903] | 42753199 [42563190, 53353956] | 1.0315434923965139 | 26835893 → 27114611 | 498814 → 501809 |

### Session 2 — primary repeated session

| Workload | Main ns/op median [range] | Branch ns/op median [range] | Δ % | Median B/op | Median allocs/op |
| --- | --- | --- | --- | --- | --- |
| Compile/filters | 12208 [11899, 12322] | 17184 [17045, 17283] | 40.76015727391875 | 12357 → 12805 | 228 → 239 |
| Compile/list | 9529 [9293, 9585] | 9902 [9766, 9930] | 3.9143666701647595 | 7259 → 7498 | 133 → 138 |
| PostgresReads/1/direct-sql | 109038 [107974, 113730] | 111105 [109371, 111500] | 1.8956693996588392 | 24270 → 24534 | 466 → 468 |
| PostgresReads/1/framework-sql | 142023 [137444, 185294] | 142127 [140342, 146595] | 0.07322757581518236 | 40221 → 40492 | 799 → 801 |
| PostgresReads/1/typed-orm | 158434 [155362, 167225] | 156030 [154121, 215681] | -1.5173510736331886 | 48154 → 48923 | 947 → 958 |
| PostgresReads/100/direct-sql | 2720616 [2674544, 2765009] | 2763578 [2747463, 2781719] | 1.5791276681457411 | 1609050 → 1636530 | 27781 → 28078 |
| PostgresReads/100/framework-sql | 4602027 [4547446, 4610756] | 4667638 [4653717, 4678805] | 1.4256978500995388 | 2664753 → 2692504 | 50096 → 50394 |
| PostgresReads/100/typed-orm | 4618243 [4605482, 4748024] | 4719841 [4715042, 4734853] | 2.199927548203928 | 2672687 → 2700939 | 50245 → 50551 |
| PostgresReads/1000/direct-sql | 24050414 [23781458, 32639036] | 24530346 [24436361, 24920711] | 1.9955249003198006 | 16322378 → 16601490 | 276521 → 279514 |
| PostgresReads/1000/framework-sql | 41973074 [41635015, 42259125] | 43097923 [42664131, 44381844] | 2.6799299951202116 | 26827997 → 27106293 | 498666 → 501651 |
| PostgresReads/1000/typed-orm | 41744245 [41642362, 42505998] | 43037895 [42257355, 43546767] | 3.0989900524012404 | 26835750 → 27114779 | 498813 → 501811 |

All ten samples in each session passed the required read agreement checks. All typed-read timing medians are within 5%; some ranges remain too wide to establish equivalence. The filters compilation miss reproduces in both sessions. Allocation increases are stable and independently fail the zero-extra-allocation target.

## Footprint

One fresh main suite, one fresh branch consumer suite, one branch Go-way suite; all committed budget assertions passed.

| Probe | Main functions | Branch functions | Δ % | Main archive bytes | Branch archive bytes |
| --- | --- | --- | --- | --- | --- |
| endpoint_plain → endpoint_plain | 1068 | 1068 | 0 | 4742260 | 4743060 |
| model_draft → model_assignments | 225 | 200 | -11.111111111111116 | 983964 | 882218 |
| model_isolated → model_isolated | 139 | 106 | -23.741007194244602 | 623286 | 454960 |
| model_type → model_type | 1159 | 915 | -21.052631578947366 | 4715308 | 3720600 |

`model_draft` → `model_assignments` preserves write-input intent but has different API reachability; it is not identical probe source. Go-way probes have no counterpart on main.

| Branch-only probe | Functions | Unchanged ceiling | Archive bytes |
| --- | --- | --- | --- |
| onequery | 825 | 924 | 3874696 |
| q_with | 1175 | 1211 | 5215440 |
| aggregate_families | 4363 | 4369 | 19421526 |
| column_number | 1253 | 1348 | 5173912 |
| column_nullable_text | 1573 | 1675 | 6398916 |
| role_input_same | 1343 | 1564 | 5391798 |

All 148 branch-only per-kind/slope/capability probe budgets passed; every measured probe and ceiling is in JSON. The one-query target uses the normative PLAN reference of 4,741 functions (30% = 1,422.3), not an invented fresh main probe.

### Full consumer models package

| Metric | Main | Branch | Branch/main % | 25% threshold | Result |
| --- | --- | --- | --- | --- | --- |
| Archive bytes | 230162040 | 70615262 | 30.680672625251322 | 57540510 | FAIL |
| Functions | 53606 | 16181 | 30.185053911875535 | 13401.5 | FAIL |

These archives were already built by the footprint run. The additional inspection used that cache, with no source changes.

## Synthetic 70-model app

Five fresh cold builds and five subsequent uncached test runs per side. Test time is **warm-build-cache test compilation/linking/execution**, not cold test time or application runtime. Cache bytes are sums of regular-file sizes, recorded after build and again after archive inspection/tests; they are not APFS allocated-block counts. Peak RSS is the maximum **individual cold-build compiler** RSS; it is not aggregate concurrent memory. The models compiler was the peak process on both sides.

| Metric (seconds or bytes as named) | Main median [range] | Branch median [range] | Δ % |
| --- | --- | --- | --- |
| cold_build_seconds | 138.64079658396076 [136.02172312501352, 139.87710995809175] | 39.406960334046744 [38.86231433402281, 39.97618695802521] | -71.57621616074464 |
| warm_build_test_seconds | 66.790734124952 [66.47775095805991, 67.19706549996044] | 21.25095091701951 [21.040591249940917, 21.71675616700668] | -68.18278583783304 |
| cache_bytes | 8913432046 [8913374713, 8913432046] | 2155863207 [2155810864, 2155863207] | -75.81332088611741 |
| cache_bytes_after_test | 10609892347 [10609892347, 10609892347] | 2636058824 [2636058824, 2636058824] | -75.15470715642691 |
| whole_archive_bytes | 8753809188 [8753809188, 8753809188] | 1944818662 [1944818662, 1944818662] | -77.78317278532847 |
| whole_archive_functions | 2186408 [2186408, 2186408] | 490791 [490791, 490791] | -77.5526342750301 |
| models_archive_bytes | 1670605162 [1670605162, 1670605162] | 453977638 [453977638, 453977638] | -72.82555756882067 |
| models_functions | 425007 [425007, 425007] | 109089 [109089, 109089] | -74.33242276009572 |
| max_compile_rss_bytes | 21673312256 [20114718720, 22165766144] | 5889507328 [5718425600, 6051119104] | -72.82599328411577 |
| handler_median_functions | 41608 [41608, 41608] | 8674.5 [8674.5, 8674.5] | -79.15184579888484 |
| handler_median_archive_bytes | 166303090 [166303090, 166303090] | 33950446 [33950446, 33950446] | -79.58519832674186 |

### Individual paired trials

| Trial | Execution order | Side | Cold build s | Warm-build test s | Post-build cache B | App archives B | Compiler peak RSS B |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | main,branch | main | 139.87710995809175 | 67.0476062920643 | 8913432046 | 8753809188 | 21673312256 |
| 1 | main,branch | branch | 39.97618695802521 | 21.040591249940917 | 2155863207 | 1944818662 | 5889507328 |
| 2 | branch,main | main | 138.64079658396076 | 66.78193104104139 | 8913432046 | 8753809188 | 22165766144 |
| 2 | branch,main | branch | 39.406960334046744 | 21.231341582955793 | 2155810864 | 1944818662 | 5848317952 |
| 3 | main,branch | main | 136.02172312501352 | 66.790734124952 | 8913374713 | 8753809188 | 21898428416 |
| 3 | main,branch | branch | 38.86231433402281 | 21.25095091701951 | 2155863207 | 1944818662 | 5969035264 |
| 4 | branch,main | main | 137.4877274170285 | 67.19706549996044 | 8913432046 | 8753809188 | 20114718720 |
| 4 | branch,main | branch | 39.55401054199319 | 21.71675616700668 | 2155810864 | 1944818662 | 6051119104 |
| 5 | main,branch | main | 139.19319954200182 | 66.47775095805991 | 8913374713 | 8753809188 | 21139980288 |
| 5 | main,branch | branch | 39.33403954200912 | 21.34126324998215 | 2155863207 | 1944818662 | 5718425600 |

The native models package is 27.17444243117932% of fresh main bytes and 25.667577239904283% of its functions: both exceed 25%. Its compiler RSS also exceeds both 5 GB and 5 GiB. Handler medians remain below 9,820 functions and 38,000,000 bytes. Whole-app build/cache/archive/test savings are reproducible across five pairs; PLAN specifies reporting rather than a numerical acceptance cutoff for those savings.

## Stage 6 microbenchmarks vs pre-4a

### Session 1 — retained noisy first session

| Workload | Pre-4a ns/op median [range] | Branch ns/op median [range] | Δ % | Median B/op | Median allocs/op |
| --- | --- | --- | --- | --- | --- |
| ChainBuild | 146 [142.6, 203] | 185.4 [179, 188.4] | 26.98630136986302 | 240 → 312 | 6 → 8 |
| ChainCompile | 1834 [1760, 1918] | 4512 [4264, 4598] | 146.01962922573608 | 3504 → 4905 | 56 → 83 |
| Hydrate1000 | 192280 [184412, 194975] | 3833008 [3818328, 3871469] | 1893.4512169752445 | 286472 → 4626650 | 5065 → 94135 |

### Session 2 — primary repeated session

| Workload | Pre-4a ns/op median [range] | Branch ns/op median [range] | Δ % | Median B/op | Median allocs/op |
| --- | --- | --- | --- | --- | --- |
| ChainBuild | 146.2 [143.3, 146.8] | 186.7 [178, 219.7] | 27.701778385772926 | 240 → 312 | 6 → 8 |
| ChainCompile | 1842 [1758, 1858] | 4520 [4418, 4674] | 145.385450597177 | 3504 → 4905 | 56 → 83 |
| Hydrate1000 | 193652 [184398, 195458] | 3840198 [3835160, 3919213] | 1883.0407122053994 | 286472 → 4626667 | 5065 → 94135 |

Every Hydrate1000 sample checked all IDs/buyers/totals, exactly 1 query/op and 1,000 rows/op. All three misses reproduce. Some repeated-session baseline/compile ranges still span more than 5%, but the failing branch ranges remain separated from the baseline: the failure does not rely on an outlier. The required pre-4a baseline predates H1 callback containment, so this comparison includes that merged-main behavior as well as later ORM work; it does not isolate advanced-SQL cost.

## Misses: likely causes and source evidence

1. **Runtime filter compilation:** the native path adds retained predicate/value storage and carries a larger typed column/binding closure through [column.go](../../database/orm/column.go):129–137. The profile contains these constructors and comparison binding; [compiler.go](../../internal/querycore/compiler.go):223–233 and [codec.go](../../database/codec/codec.go):60 invoke isolated binding callbacks. Allocation count rises from 228 to 239. CPU profiles are dominated by Darwin scheduler/handoff frames, so a precise causal split of the 40% timing gap is not established; this is a likely hot-path explanation, not an isolated attribution experiment.
2. **Extra read allocations:** [codec.go](../../database/codec/codec.go):83–84 now delegates `Decode` to `scanner.Scan`; the latter creates another captured decoded value at :114–119. Annotated exact-rate profiles show allocation moving into the scanner and a net extra allocation per nested decode. `Validated` (:142) and non-NULL `Nullable` (:169) call `Decode`. The benchmark has two validated fields plus two half-present nullable fields, explaining approximately three extra allocations per row in both direct SQL and ORM controls. Repeated-session typed-read increases are 11 / 306 / 2,998 allocations for 1 / 100 / 1,000 rows.
3. **ChainBuild:** the two added allocations are visible as `RetainComparisonValues` at [literal_state.go](../../internal/querycore/literal_state.go):24 and `RetainPredicate` at [predicate_state.go](../../internal/querycore/predicate_state.go):14, reached from `comparison` at [column.go](../../database/orm/column.go):137. Annotated allocation profiles identify both holders. The timed path grows from 240 B/6 allocations to 312 B/8 allocations.
4. **ChainCompile and Hydrate1000 vs pre-4a:** successful codec/binding/scanning callbacks now cross [callback/invoke.go](../../internal/callback/invoke.go):39–46. Every isolated callback creates a channel/goroutine and constructs its initial failure diagnostic (:42), via [safeerror/diagnostic.go](../../internal/safeerror/diagnostic.go):21–27. The final hydration allocation profile attributes 22.92% flat objects to `Isolated` and 44.98% cumulative to `WrapFault`; these are sampled diagnostic observations, not extra benchmark trials. Before H1, codec Bind/Decode/Scan called their functions directly. SQL scanner containment is also in [sqlcallback/value.go](../../internal/sqlcallback/value.go):15. This explains the dominant allocation/handoff cost on the fake-driver path without network I/O.
5. **Models footprint and compiler RSS:** residual per-model generic instantiation/wrapper work remains above target. The native consumer archive still has 2,483 `Codec` family symbols, 1,000 `Query` family symbols and 720 `RowExpression` family symbols (value/pointer families combined); raw family counts are in JSON. [column.go](../../database/orm/column.go):123–124 retains typed codec methods, and the consumer declaration/aggregate graph reaches them through [models/orm.go](../../tests/fixtures/consumer/models/orm.go):45 and [orm_relations.go](../../tests/fixtures/consumer/models/orm_relations.go):55. The synthetic generator places 70 typed declarations in one models package ([goway_synthetic.py](../../tools/goway_synthetic.py):128, :219). That package retains 109,089 functions / 453,977,638 archive bytes and is the largest compiler process in every pair. This is evidence of the remaining instantiation footprint; no compiler heap profile was taken to assign RSS to an individual generic family.

Diagnostic profiles ran **after all paired measurements** using the already-built pinned binaries and are excluded from the tables. Raw profile files, annotated source/top reports and commands are retained; text is embedded in JSON. No framework implementation change, budget adjustment, regenerated framework output or discarded sample was used to improve a verdict.

## Scope and retained evidence

The measurement commands completed successfully; no benchmark returned a correctness error or missing workload. This turn did not rerun functional acceptance targets (full SQL goldens, catalogue, eager/N+1, client contracts, editor probes, full verify/races); they are marked not rerun rather than borrowing historical green status.

JSON retains every benchmark sample and stdout/exit record, source/tool hashes, all 148 Go-way probe measurements and budgets, all synthetic package metrics and trial conditions, model compiler resource records, profile text and cleanup receipts. Repeated filename-to-hash maps are losslessly deduplicated under `source_sets`; `$sha256_map` references reconstruct them. Raw artifacts remain at `/private/tmp/goway-stage11-20261027-laneC4`. The temporary baseline checkouts and all task caches are gone. No commit was made.


## Final release candidate (30578f82)

**Measured targets are not all met.** Runtime and read allocations pass; the model-package ratios, 70-model compiler RSS and strict ChainBuild target remain separate gates. No implementation or budget changed.

### Conditions

- Main `b8816902`; RC `30578f82`; Stage 6 baseline `35f8d45`. Native macOS, Apple M4 Max, 16 logical CPUs, 64 GiB RAM, `go1.27.1`.
- Same fixed environment as the earlier run: `GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`, `GOENV=off`, `GOTOOLCHAIN=local`, `GOGC=100`, `GOMEMLIMIT=off`, empty `GODEBUG`; synthetic commands additionally use readonly modules and the existing RSS wrapper.
- Runtime and Stage 6 each have two complete sessions of five adjacent main/baseline–RC pairs, fresh process per sample, 1s, benchmem. The first sessions had timing dispersion near/above 5%; both sessions and all samples remain. Session 2 is primary. Some ranges remain noisy; medians/ranges are observations, not confidence intervals.
- PostgreSQL agreement checks validated all eight decoded fields and one ORM SELECT before each timed runtime sample. An initial sandbox-only agreement attempt could not connect and produced no timing sample; the full timed session resumed with network access, reusing the built binaries. No schema reset or service restart.
- Spotlight/GUI activity was observed despite no competing heavy jobs from this lane. Load averages, command statuses and source hashes are retained per trial. Branch dirty flags reflect only the restored historical evidence files; measured Go/module/harness source hashes did not change.
- Synthetic: five fresh cold paired trials, 70 models / 40 handlers / 5 reports, seed 1; orders main,RC / RC,main / main,RC / RC,main / main,RC. Generation is outside timing. Each side/trial has its own cold cache; subsequent test time uses the warmed build cache.
- Disk checked before heavy runs with a conservative 40 GiB reserve; the 85% stop limit was respected. Completed task caches and both temporary baseline worktrees were removed. No shared lane cache was used. Raw reports/logs/profiles and synthetic source inputs remain at `/private/tmp/goway-stage11-rc30578f82-laneC5`.

### PLAN §6 targets

| Target | Verdict | Criterion |
| --- | --- | --- |
| model-reading package | PASS | <=1158 functions |
| one-query package | PASS | <=30% of PLAN reference 4741 |
| full consumer models | FAIL | functions and bytes <=25% of fresh main |
| synthetic models | FAIL | functions and bytes <=25% of fresh main |
| 70-model compiler RSS | FAIL | every trial <=5000000000 bytes (5 GB) |
| per-model cost | PASS | all unchanged per-kind/slope footprint budgets |
| whole-app cache/compile/archive/test reporting | PASS | five cold paired trials, fresh main |
| ORM runtime | PASS | within +5% or faster than main |
| read allocations | PASS | 0 extra allocations for every read family |
| ChainBuild | FAIL | <=pre-4a |
| ChainCompile | PASS | within +5% or faster, no extra allocations |
| Hydrate1000 | PASS | within +5% or faster, no extra allocations |
| handler package growth | PASS | median package <=9820 functions / 38000000 bytes |
| eager loading / N+1 | NOT RERUN | outside measurement-only scope; no historical pass substituted |
| SQL golden suite | NOT RERUN | outside measurement-only scope; no historical pass substituted |
| typing catalogue | NOT RERUN | outside measurement-only scope; no historical pass substituted |
| Go to TS contracts | NOT RERUN | outside measurement-only scope; no historical pass substituted |
| editor | NOT RERUN | outside measurement-only scope; no historical pass substituted |
| make verify + PostgreSQL races | NOT RERUN | outside measurement-only scope; no historical pass substituted |

Functional acceptance rows are explicitly not rerun in this measurement-only task; no historical green result is presented as a fresh pass. Runtime agreement checks cover only their stated workload. Faster than the lower -5% band counts as an improvement.

### Runtime against main

#### Session 1 (retained first session)

| Workload | Main ns/op median [min, max] | RC ns/op median [min, max] | Δ % | B/op median | allocs/op median |
| --- | --- | --- | --- | --- | --- |
| Compile/filters | 12081 [11798, 15830] | 4417 [4379, 4752] | -63.43845708136744 | 12357 → 5921 | 228 → 99 |
| Compile/list | 9503 [9463, 9608] | 4505 [4477, 4536] | -52.59391771019678 | 7259 → 4280 | 133 → 69 |
| PostgresReads/1/direct-sql | 109345 [107770, 110360] | 95349 [94905, 96327] | -12.79985367415063 | 24271 → 12343 | 466 → 224 |
| PostgresReads/1/framework-sql | 140611 [138856, 144440] | 111326 [109915, 113757] | -20.826962328694055 | 40220 → 18207 | 799 → 344 |
| PostgresReads/1/typed-orm | 159831 [157482, 163094] | 117352 [116425, 118426] | -26.577447428846725 | 48153 → 22715 | 947 → 417 |
| PostgresReads/100/direct-sql | 2835371 [2719323, 3658069] | 1762163 [1755012, 1797681] | -37.85070807312341 | 1609029 → 683358 | 27780 → 8950 |
| PostgresReads/100/framework-sql | 4639754 [4557739, 4665308] | 2270321 [2224989, 2279784] | -51.06807386770937 | 2664733 → 934931 | 50096 → 14417 |
| PostgresReads/100/typed-orm | 4667048 [4642458, 4719769] | 2184739 [2132596, 2230060] | -53.18798949571549 | 2672674 → 782517 | 50245 → 12114 |
| PostgresReads/1000/direct-sql | 24034096 [23863740, 24793510] | 14969687 [14615087, 19412049] | -37.71479068736349 | 16322456 → 7109069 | 276522 → 88907 |
| PostgresReads/1000/framework-sql | 42043144 [41674031, 42549989] | 19160716 [18757167, 19853859] | -54.426062903383254 | 26827981 → 9592510 | 498666 → 142973 |
| PostgresReads/1000/typed-orm | 41697676 [41693879, 42274193] | 17971936 [17933460, 18206182] | -56.89943007854923 | 26835641 → 8014616 | 498812 → 119071 |

#### Session 2 (primary repeated session)

| Workload | Main ns/op median [min, max] | RC ns/op median [min, max] | Δ % | B/op median | allocs/op median |
| --- | --- | --- | --- | --- | --- |
| Compile/filters | 12110 [11990, 12437] | 4359 [4212, 4454] | -64.00495458298927 | 12357 → 5921 | 228 → 99 |
| Compile/list | 9485 [9472, 9606] | 4570 [4550, 4579] | -51.8186610437533 | 7259 → 4280 | 133 → 69 |
| PostgresReads/1/direct-sql | 108840 [107060, 154680] | 96772 [95027, 98842] | -11.087835354649023 | 24271 → 12345 | 466 → 224 |
| PostgresReads/1/framework-sql | 141480 [140521, 153316] | 109313 [107026, 111807] | -22.73607577042691 | 40219 → 18207 | 799 → 344 |
| PostgresReads/1/typed-orm | 158781 [157376, 163624] | 116335 [112609, 118121] | -26.732417606640592 | 48156 → 22718 | 947 → 417 |
| PostgresReads/100/direct-sql | 2736423 [2616929, 3515561] | 1783736 [1775425, 1786840] | -34.81504869678409 | 1609033 → 683364 | 27780 → 8950 |
| PostgresReads/100/framework-sql | 4656839 [4577341, 4666384] | 2269227 [2255242, 2923862] | -51.27108753384002 | 2664774 → 934928 | 50097 → 14417 |
| PostgresReads/100/typed-orm | 4672612 [4606986, 4715510] | 2180639 [2149753, 2204844] | -53.331477126712 | 2672672 → 782520 | 50244 → 12114 |
| PostgresReads/1000/direct-sql | 24050781 [23784646, 24762297] | 14681185 [14614634, 14885659] | -38.95755401872396 | 16322567 → 7109069 | 276524 → 88907 |
| PostgresReads/1000/framework-sql | 41818733 [41065631, 51010072] | 18637829 [18504431, 19004790] | -55.43186590564568 | 26827704 → 9592511 | 498662 → 142973 |
| PostgresReads/1000/typed-orm | 42773969 [41630719, 43269006] | 18104411 [17802675, 18226273] | -57.67423172724514 | 26835807 → 8014585 | 498813 → 119071 |

Every timed process passed agreement and all eleven workloads. Improvements repeat across both sessions; all repeated-session RC runtime ranges lie below their main counterparts, despite noisy controls. Read allocation counts decrease for all three execution paths. These timings include PostgreSQL, decoding and containment; they do not isolate advanced SQL or demonstrate write/eager-load performance.

### Stage 6 against pre-4a

#### Session 1 (retained first session)

| Workload | Pre-4a ns/op median [min, max] | RC ns/op median [min, max] | Δ % | B/op median | allocs/op median |
| --- | --- | --- | --- | --- | --- |
| ChainBuild | 145.9 [144.7, 148] | 151 [146.8, 194.1] | 3.4955448937628475 | 240 → 176 | 6 → 4 |
| ChainCompile | 1847 [1764, 1862] | 1850 [1782, 2002] | 0.16242555495398214 | 3504 → 3456 | 56 → 52 |
| Hydrate1000 | 195647 [188014, 196887] | 175716 [173631, 178607] | -10.187224951059815 | 286474 → 128417 | 5065 → 109 |

#### Session 2 (primary repeated session)

| Workload | Pre-4a ns/op median [min, max] | RC ns/op median [min, max] | Δ % | B/op median | allocs/op median |
| --- | --- | --- | --- | --- | --- |
| ChainBuild | 146.8 [141.1, 147.6] | 151.7 [146.4, 153.5] | 3.3378746594005193 | 240 → 176 | 6 → 4 |
| ChainCompile | 1807 [1761, 1837] | 1845 [1787, 1848] | 2.102933038184829 | 3504 → 3456 | 56 → 52 |
| Hydrate1000 | 192336 [191215, 197200] | 174962 [174788, 227467] | -9.033150320272853 | 286473 → 128418 | 5065 → 109 |

Hydrate1000 verified every ID/buyer/total, 1,000 rows and exactly one SELECT per operation. ChainCompile meets the median ±5% criterion; overlapping ranges prevent claiming a measurable timing difference. ChainBuild remains above the strict no-slower threshold in both session medians, with overlapping ranges: **FAIL to establish the target; causal regression is inconclusive**, rather than proof that a particular change costs 3.3%.

### Footprint and full consumer models

One fresh main consumer suite, one RC consumer suite and all 148 RC Go-way probes passed their committed budgets. No existing ceiling was raised. T/t symbols include compiler wrappers/generic instantiations; archive bytes are not executable size.

| Probe | Main functions | RC functions | Main archive bytes | RC archive bytes |
| --- | --- | --- | --- | --- |
| endpoint_plain → endpoint_plain | 1068 | 1068 | 4742260 | 4743066 |
| model_draft → model_assignments | 225 | 200 | 983964 | 884184 |
| model_isolated → model_isolated | 139 | 106 | 623286 | 454968 |
| model_type → model_type | 1159 | 915 | 4715308 | 3722546 |

`model_draft` → `model_assignments` preserves write-input intent with different API reachability. Go-way has no main counterpart; the one-query gate uses PLAN’s fixed 4,741 reference.

| RC-only probe | Functions | Unchanged ceiling | Archive bytes |
| --- | --- | --- | --- |
| onequery | 822 | 924 | 3843036 |
| q_with | 1170 | 1211 | 5183944 |
| aggregate_families | 4352 | 4369 | 19270826 |

| Full models metric | Main | RC | RC/main % | 25% threshold |
| --- | --- | --- | --- | --- |
| archive_bytes | 230162040 | 69921030 | 30.37904512837999 | 57540510 |
| functions | 53606 | 16129 | 30.088049845166587 | 13401.5 |

Archives were inspected from the completed footprint build cache, without source changes. Full metrics and symbol-family counts are in JSON.

### Synthetic 70-model application

Five cold builds and five warmed-cache uncached test invocations per side. Cache sizes sum regular-file bytes; RSS is the maximum individual cold-build compiler process, not concurrent aggregate RSS. Test time includes compilation/linking/execution with the ordinary cache warmed. App archive sums exclude dependency archives and are not linked executable sizes.

| Metric (seconds/bytes as named) | Main median [min, max] | RC median [min, max] | Δ % |
| --- | --- | --- | --- |
| cold_build_seconds | 142.2580872499384 [140.63748337503057, 145.9208761670161] | 40.424045874970034 [38.9372196670156, 41.4787281670142] | -71.5840085745371 |
| warm_build_test_seconds | 68.73212937498465 [67.61681775003672, 69.37030683306511] | 21.750247791060247 [20.829862333950587, 22.267218708992004] | -68.35505026710791 |
| cache_bytes | 8913447778 [8913390141, 8913447778] | 2139295928 [2139243349, 2139295928] | -75.99923193267404 |
| cache_bytes_after_test | 10609908653 [10609908653, 10609908653] | 2613945419 [2613945419, 2613945419] | -75.3631675399873 |
| whole_archive_bytes | 8753824534 [8753824534, 8753824534] | 1928963254 [1928963254, 1928963254] | -77.96433722759835 |
| whole_archive_functions | 2186408 [2186408, 2186408] | 489324 [489324, 489324] | -77.6197306266717 |
| models_archive_bytes | 1670605730 [1670605730, 1670605730] | 448431586 [448431586, 448431586] | -73.15754531741011 |
| models_functions | 425007 [425007, 425007] | 108686 [108686, 108686] | -74.42724472773389 |
| max_compile_rss_bytes | 21374517248 [20414005248, 21816885248] | 5934563328 [5523095552, 6324060160] | -72.23533397669932 |
| handler_median_functions | 41608 [41608, 41608] | 8649 [8649, 8649] | -79.2131320899827 |
| handler_median_archive_bytes | 166303378 [166303378, 166303378] | 33706509 [33706509, 33706509] | -79.73191560787178 |

| Synthetic models metric | RC/main % | Target | Verdict |
| --- | --- | --- | --- |
| models_archive_bytes | 26.84245468258989 | <=25% | FAIL |
| models_functions | 25.5727552722661 | <=25% | FAIL |

#### Individual paired trials

| Trial | Order | Side | Cold build s | Warm-cache test s | Post-build cache bytes | App archive bytes | Compiler RSS bytes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | main,RC | main | 140.63748337503057 | 68.73212937498465 | 8913447778 | 8753824534 | 21484158976 |
| 1 | main,RC | RC | 40.747347666998394 | 21.40526983293239 | 2139295928 | 1928963254 | 6324060160 |
| 2 | RC,main | main | 143.29339091607835 | 68.62308720801957 | 8913447778 | 8753824534 | 20541734912 |
| 2 | RC,main | RC | 39.74133241700474 | 21.959731957991607 | 2139243349 | 1928963254 | 5934563328 |
| 3 | main,RC | main | 142.2580872499384 | 69.37030683306511 | 8913390141 | 8753824534 | 21374517248 |
| 3 | main,RC | RC | 40.424045874970034 | 21.750247791060247 | 2139295928 | 1928963254 | 5673762816 |
| 4 | RC,main | main | 141.34607441700064 | 67.61681775003672 | 8913447778 | 8753824534 | 20414005248 |
| 4 | RC,main | RC | 38.9372196670156 | 20.829862333950587 | 2139243349 | 1928963254 | 6144753664 |
| 5 | main,RC | main | 145.9208761670161 | 69.20729091705289 | 8913447778 | 8753824534 | 21816885248 |
| 5 | main,RC | RC | 41.4787281670142 | 22.267218708992004 | 2139295928 | 1928963254 | 5523095552 |

### Remaining misses and source evidence

1. **Full consumer and synthetic model-package footprint:** residual generic model/codec/query families remain above the 25% target. The RC consumer archive retains 2763 `Codec[`-containing T/t symbols and 720 `RowExpression[`-containing symbols (overlapping families, not additive). Typed codec closures remain in [database/orm/column.go](../../database/orm/column.go):125, reached by the consumer declarations at [models/orm.go](../../tests/fixtures/consumer/models/orm.go):45 and relation graph at [orm_relations.go](../../tests/fixtures/consumer/models/orm_relations.go):55. The synthetic app deliberately places all 70 native declaration families in one package, [tools/goway_synthetic.py](../../tools/goway_synthetic.py):128 and :219. This is measured remaining reachability, not attribution of every byte to one family.
2. **70-model compiler RSS:** per-package resource records identify the models compiler as the peak. The same concentrated generic declaration graph remains the likely cause; no compiler heap profile was taken to assign bytes to a specific generic family. RSS records include `/usr/bin/time -l`, package and exit status; cold-build headline values are captured before tests, which may compile models again.
3. **ChainBuild strict target not established:** the measured path constructs/retains a predicate and copies query/clause state at [column.go](../../database/orm/column.go):139, [querycore/predicate_state.go](../../internal/querycore/predicate_state.go):20 and [query.go](../../database/orm/query.go):123/:142. The RC also marks its cached binder at [querycore/ast.go](../../internal/querycore/ast.go):91; these are likely contributors, not isolated proof of the small median gap. Separate fresh CPU/allocation profiles (excluded from acceptance) identify comparison retention and clause slices, but most CPU samples are Darwin `kevent`/`madvise`/scheduler frames. Timing ranges overlap and background indexing was present. The defensible result is an unproven strict target, with causal attribution unresolved; allocations improve from 6 to 4 and bytes from 240 to 176.

The containment changes explain the direction of the larger measured runtime/read gains: sealed native codecs in [database/codec/codec.go](../../database/codec/codec.go):58, collection-owned adapters at [internal/sqlcallback/value.go](../../internal/sqlcallback/value.go):63 and [database/orm/query.go](../../database/orm/query.go):275, and lazy failure creation at [internal/callback/invoke.go](../../internal/callback/invoke.go):30. The paired benchmark measures their combined effect plus ORM differences against each pinned baseline; it does not isolate each optimization. No source change was made to improve a verdict.

Fresh excluded typed-read CPU/allocation profiles support a handoff/allocation limiter: main’s `safeerror.WrapFault` accounts for 44.11% cumulative sampled allocation objects; the RC instead retains application/driver isolation (`callback.Isolated`, 51.26% flat, and protected-row column scanning). CPU samples on both sides are dominated by pthread condition handoffs. Percentages are within-profile sampled object shares, not per-operation estimates, additional timing trials or a causal split of each optimization.

### Evidence preservation

The earlier Markdown is an unchanged prefix, and every earlier JSON key/value is preserved. This section’s raw records live under `final_release_candidates[0]`; its `$sha256_map` references resolve only through that record’s `source_sets`. Every benchmark sample, command stdout/status, source hash, footprint probe, synthetic package metric, resource record and cleanup receipt is retained. Profiled runs are diagnostics only and excluded from all acceptance medians. No commit was made.
