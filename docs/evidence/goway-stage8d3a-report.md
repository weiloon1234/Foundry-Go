# Stage 8d3a — transfer and simplify

Base `3d6f0379`; decisions 7 and 9. The engine and bridges remain for 8d3b. Cursor wire identity and protected outputs are unchanged; decision 8 belongs to 8d3c. No commit.

## Caller and assertion closure

Parsed imports across root, nested modules, tests and testdata leave only the **13 bridge files** listed in [remaining imports](goway-stage8d3a-remaining-imports.json). Ordinary callers use native declarations. Root adapter/error, adjustment and identity tests retain their native behavior assertions; fixtures, editor probes, guide excerpts and measurement inputs use the canonical operations.

[The transfer ledger](goway-stage8d3a-transfers.json) names replacements for **97 query test files / 262 test, fuzz and benchmark entry points**. It combines transferred assertions with existing native/shared tests, rather than preserving the old façade merely to test it. Notable direct transfers:

| Old query assertions | Native/shared replacement |
| --- | --- |
| Compiler binding, precedence, immutable derivation, resource rejection | `orm/compiler_transfer_test.go`: `TestCompilerBindsValuesQuotesNamesAndPreservesPrecedence`, `TestQueryAndBindingsAreIndependent`, `TestCompilerRejectsInvalidBoundariesAndResources` |
| Recursive working-table restrictions and dependencies | `orm/rows/recursive_transfer_test.go`: the five `TestRecursiveCTE*` tests |
| Named windows, inheritance and SELECT isolation | `orm/rows/window_named_transfer_test.go`: the three `TestNamedWindow*` tests |
| Cyclic SELECT/CTE/correlation/window analysis | `querycore/structural_bounds_test.go`: five bounded-analysis tests |
| Interval expansion and shared planning budget | `orm/rows/interval_operation_test.go`: four transferred tests |
| Schema literals and PostgreSQL round trips | `querycore/schema_literal{,_postgres}_test.go` |
| EXPLAIN ownership, malformed JSON, fuzzing, transaction execution and locks | `querycore/explain_decode_test.go`; `orm/explain{,_postgres}_test.go` |
| Cursor codec classification, cause identity, cycles and bounds | `querycore/cursor_boundary_transfer_test.go`, `result_cursor_transfer_test.go`, `cursor_token_test.go`; `orm/cursor_output_test.go`; `orm/rows/cursor_fingerprint_test.go` |
| Abnormal transaction error inspection and reconciliation candidates | `orm/outcome_graph_test.go`: `TestCustomTransactorErrorInspectionRetainsOnlyReconciliationCandidate` |
| Geographic extremes, NULL and antimeridian | `orm/rows/geographic_transfer_test.go`: both geographic tests |
| Loaded aggregate NULL versus unloaded; signed-zero key equality | `relation/loaded_test.go`; `querycore/interval_key_test.go` |
| Inactive relation compilation allocations and fresh diagnostic scopes | `querycore/statement_shape_alloc_test.go` |

Generated Memo repeat-panic behavior and the arbitrary-compiler FoundryExplain bridge retire under decision 7. Native declaration and preflight tests cover their successors; the ledger explicitly records that these two contracts are not equivalent transfers. The old public package identity also retires. Native typed projections replace separately supplied generated getters. Benchmark workloads are identified separately in the ledger; historical timings are not relabelled as native measurements. Empty native junctions require an owner; owned empty membership retains the TRUE/FALSE SQL identities.

Archived **160 dormant consumer negatives** byte-for-byte as `invalid.go.txt`; retirement digests remain checked. Twelve active consumer rules now have native counterparts. Twelve goway legacy conversion cases became native phase/owner cases, and two old text cases reuse their existing native counterparts. [Negative ports](goway-stage8d3a-negative-ports.tsv) records the exact replacements. The archive reader rejects duplicate active/archive sources and unsupported suffixes.

## Surface

[Exact snapshot subtractions](goway-stage8d3a-surface-subtractions.tsv) record **39 removed lines**, including nine replaced alias spellings. Unrelated entries remain. [Source declarations](goway-stage8d3a-api-removals.tsv) record all **30 removed spellings**, including constants and APIs outside the ORM snapshot. No Query/Table methods were added.

| Removed route | Retained route |
| --- | --- |
| `Predicate.Not` | `orm.Not` |
| `rows.Compile/Preflight/Each/Count` | `CompileRead/PreflightRead/EachRead`; `Read(q).Count` |
| `RecordRead.All`, `SetQuery.All`, `ValueSetQuery.All` | `ReadRecord/ReadSet/ReadValues(...).All` |
| `ValueCursorQuery.Key` | `expr.Group(cursor.Value())` |
| `expr.Select` | Row-carrier `.Value()`; `expr.Row(e).Value()` for an `Expression` |
| Six JSONPath conveniences | Nullable JSON expression composition |
| `expr.JSONKind` and six constants | `database.JSONKind` and constants |
| ORM morph aliases | `model.MorphName/Morphable` |
| `Load/LoadMissing` | `LoadFrom/LoadMissingFrom` with the actual parent query |
| `Config.DownloadTimeout` | `Manager.DownloadTimeout`, including nil behavior |
| `model.NewID` | `NewIDAt` with an explicit clock sample |
| `jobs.LegacyEnvelope` | `EnvelopeV1`; unchanged v1 bytes/default decoding, no alias |

`Table.Find/Require/Update/Delete`, scoped keyed writes, aggregate `.Value()`, loaded-slot policies and retrieval dispatch remain. Streaming has one delivery owner: `EachRead` passes source/decoder to the private streaming helper without instantiating an intermediate `Query[R,R]`. Functions/types/constants absent from the old ORM snapshot (`Load`, `NewID`, job envelope, timeout configuration and scalar aliases/constants) are explicitly listed above rather than represented by invented snapshot lines. Five obsolete phase-guard exceptions for removed aliases were subtracted.

## Footprint

[Measurement provenance and sources](goway-stage8d3a-footprint.json) record `T/t` symbols in Go package archives on Go 1.27.1 darwin/arm64, same base/toolchain/settings; they are not runtime benchmarks. Every surviving ceiling is unchanged.

| Probe | Base | After | Delta | Existing ceiling |
| --- | ---: | ---: | ---: | ---: |
| q_with | 1,211 | 1,173 | -38 | 1,211 |
| aggregate_families | 4,365 | 4,339 | -26 | 4,369 |
| rows_each | 899 | 990 | +91 | 1,034 |
| read_record | 1,002 | 934 | -68 | 1,153 |
| read_set | 1,050 | 982 | -68 | 1,208 |
| result_reads | 1,387 | 1,356 | -31 | 1,554 |
| record_sets | 1,294 | 1,455 | +161 | 1,482 |
| value_sets | 2,239 | 2,416 | +177 | 2,565 |
| result_cursor | 2,232 | 2,179 | -53 | 2,564 |
| json_paths | 6,168 | 5,680 | -488 | 7,068 |
| extension_slots | 2,104 | 2,436 | +332 | 2,478 |

Growth in record/value-set probes is the canonical Result adapter's reachable method family. `rows_each` now exercises the *Read adapter; removing the unnecessary intermediate generic query keeps it below its existing ceiling. The old extension probes were captured from an isolated `git archive HEAD` base, then archived as text: matching **8,452** (ceiling 9,697), files **7,264** (ceiling 8,331). [Retirement evidence](goway-stage8d3a-retired-footprints.json) keeps their original probe hashes and budgets; neither is repurposed as a smaller workload. No new footprint probe or ceiling.

The required `LoadMissingFrom` query construction initially made `extension_slots` **2,472**, above its stricter quarter-of-historical-baseline threshold. The final **2,436** passes both its ordinary **2,478** ceiling and `count × 4 < 9,749`. The workload and assertion are unchanged. Slot-independent copying now instantiates only per model, set-write execution uses the existing declaration-owned observer check, and callback constructors remain compiled once in their non-generic owners. The redundant private existing-parent load wrapper was folded into its only caller. No consumer helper hides work in another package; no public layout or signature changed for this reduction.

## Verification

Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r18 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. PostgreSQL uses the existing private test configuration and required mode. Checks below use the final source unless explicitly pending. Segmented runs resume failed checks and unrun packages; passed unrelated packages are not repeated.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS (full root, final source) |
| Root `make test` | PASS; final full root with PostgreSQL required, followed by final rows race coverage for the added capture regression |
| Touched-package PostgreSQL races | PASS: 47 root packages plus all four fixture modules |
| `make fixture-check` (including replay, negatives and footprint) | PASS, segmented: corrected three stale migration-guide excerpts, then reran migration and every remaining package |
| `python3 tools/goway_catalogue.py` | PASS: 1,032 rows, 587 ported / 582 distinct native cases |
| Catalogue archive-reader regression | PASS |
| `make generate-check` | PASS; all generation directories checked as the agent-smoke prerequisite, retained outputs unchanged |
| `make docs-check` | PASS (including final report links) |
| Real `make agent-smoke` | PASS, segmented: all probes passed except one stale completion expectation; corrected `orm-report-compile` passed its isolated real-gopls rerun |
| `make fmt-check` | PASS (final whole-tree check) |

[Current-source census refresh](goway-stage8d3a-census.json) records reviewed fixture hash updates. Frozen bundles and receipts are untouched. JSON containment captures fragments with the existing `value.NewJSON` owner: the regression checks cause identity, Invalid classification, four redacted formatting forms and zero-snapshot rejection before SQL, including LIMIT 0. The baseline engine and all its standalone assertions remain executable until 8d3b. [Slice evidence](goway-stage8d3a.json) records commands, logs and changed-source hashes.

Shared registries/owners touched: ORM method-surface test and snapshot, native phase-conversion allowlist, both fixture compiler registries, consumer retirement manifest, goway footprint registry and current-source oracle census, compile-fail mapping TSV, catalogue tooling, real-gopls probes, ORM read/write/extension callback owners and the shared querycore assertion owners. The evidence source list names every changed file; no lead-owned shared page, protected generated artifact or frozen bundle was edited.

## Review 41 assertion transfers

The eight ledger gaps now have direct native/shared assertions. The ledger's affected rows name these replacements explicitly; nearby family coverage no longer stands in for the missing invariant.

| Gap | Direct replacement and assertion |
| --- | --- |
| 1. Literal case-insensitive text | `rows/text_insensitive_transfer_test.go`: six field/computed/grouped, nullable/non-null paths check escaped SQL/bindings. PostgreSQL checks literal `%_!`, the distinct pattern result and NULL. |
| 2. Invalid morph declarations | `orm/relation_morph_transfer_test.go`: namespaced/hyphen names and missing discriminators fail Check, eager compilation/loading, EXISTS and single/bulk pivot writes in both directions. Valid declarations pass; invalid ones perform no SQL or transaction. |
| 3. Pivot privacy/defaults | Same file: forward/reverse carriers remain redacted through private/nested/interface wrappers and error formatting. Real single/bulk insert bindings and hydrated pivots retain both endpoints and the discriminator. |
| 4. Grandparent grouping | `rows/correlation_grouping_transfer_test.go`: nested EXISTS in HAVING rejects the ungrouped grandparent, then compiles with that key grouped. |
| 5. DISTINCT budgets | `querycore/distinct_budget_test.go`: expression and binding comparison limits fail independently; the last permitted comparison succeeds. |
| 6. Source writes | `ormcore/source_match_test.go`: one/zero/ambiguous counts and unchanged scan-error identity. `querycore/source_write_transfer_test.go`: user CTE `foundry_write_source`, private `_2` allocation and binding preservation. |
| 7. Keyset-file guarantees | `orm/count_order_transfer_test.go`: COUNT strips irrelevant order, retains LIMIT/OFFSET order, and leaves the source unchanged. `rows/relative_time_transfer_test.go`: separately constructed transaction-relative windows compile to identical SQL/bindings. |
| 8. Materialized winners | `rows/materialized_winner_transfer_test.go`: inner DISTINCT ON/descending order/limit and bindings survive the opposite outer reorder; the original report remains unchanged. |

The eight public JSON-expression examples now use the retained `Row(Value(...)).Value()` composition. No runtime or API change is required. No fixture source changed in this correction: all **551** current-source census hashes match. Frozen bundles and receipts remain untouched.

All **16** isolated fault-injection runs failed at the intended regression assertions; every production file was restored byte-for-byte before acceptance. [Review evidence](goway-stage8d3a-review-41.json) records each temporary change, command, failure log and source hash.

Repeated acceptance **PASS**: build, full-root vet, root `make test`, touched-package PostgreSQL races, `make fixture-check`, catalogue, explicit footprint test, docs and formatting. All 11 previously reported footprint counts are unchanged (including `q_with` **1,173**, aggregates **4,339**, extensions **2,436**); no ceilings changed. The added coverage does not change the public surface or the 8d3b deletion scope.

## Review 42 complete assertion audit

All **97 ledger rows** were read against the actual legacy assertions and updated with per-entry audit markers and invariant summaries. The row-level replacement lists now contain only the union of their exact per-entry replacements. Of **262 entry points**, **260** have native/shared transfers; **two** retire mechanisms required to disappear under decision 7: generated `Memo` panic replay and the arbitrary-compiler `FoundryExplain` bridge. Their entries explicitly distinguish native successor coverage from equivalent contracts.

| Reported gap | Direct replacement |
| --- | --- |
| Through aggregate with filter-only CTE | `querycore/relation_aggregate_filter_test.go:TestAggregateFilterRelationQualificationAndCTEDiscovery`: filter-only discovery, target qualification, cardinality, ordered bindings and immutable derivation. |
| Empty membership | `querycore/empty_membership_transfer_test.go`: valid operands discard computed bindings without disturbing surrounding parameters; invalid operands still fail for empty IN/NOT IN. |
| Canonical planning | `querycore/value_plan_isolation_test.go:TestCanonicalValuePlanningIsIsolated`: preexisting compiler bindings survive, canonical placeholders start independently, and SQL-looking parameter text remains verbatim. |

The full audit strengthened native coverage across scopes, relation hydration/write validation, mutation inputs, source conventions, soft deletion, timestamp clocks, scalar/temporal/JSON expressions, locks, joins, windows, CTEs, conflicts, cursors, pagination, transaction composition and callback ownership. It exposed two runtime gaps: plain row-column ordering lost its column identity at the ORM bridge (preventing native cursors), and explicit soft-delete planning did not reject a model without soft-delete metadata. Both fixes are at their existing shared planning owners.

`tools/goway-retire/transfers_test.go` checks the full legacy entry-point census, source hashes, per-entry audit metadata and actual native/shared replacement symbols. Its negatives reject missing audits, omitted source tests, empty invariant summaries, stale hashes and nominal replacement names. Semantic equivalence remains the recorded manual assertion audit. At 8d3b, archive this completed ledger with its source hashes and retire its source-existence check together with the legacy test files.

[Review 42 evidence](goway-stage8d3a-review-42.json) retains all **44** successful mutation recipes and **128** source fingerprints, rechecked after acceptance. All temporary production mutations were restored before the gate. Earlier completed r18 mutation checks were retained; all remaining Go commands used **r19**, `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

Current-source acceptance **PASS**: `go build ./...`, `go vet ./...`, root `make test`, PostgreSQL-required races for ORM/expr/rows/querycore/ormcore/lifecore and the ledger guard, `make fixture-check`, catalogue, explicit footprint, `make docs-check` and `make fmt-check`. The three transferred benchmark workloads also pass a one-iteration correctness smoke run; no timing comparison is claimed.

All previously reported footprint counts remain unchanged, including `q_with` **1,173/1,211**, aggregate families **4,339/4,369** and extension slots **2,436/2,478**. No ceiling or public surface entry changed in this review. All **551** fixture census digests still match; no fixture source needed a refresh and frozen bundles/receipts remain untouched.

## Docs notes and 8d3b handoff

Lead-owned CHANGELOG: note that plain expression-column orders retain native model-cursor eligibility and explicit soft-delete plans reject models without soft-delete metadata. Record the approved removal table above and `EnvelopeV1` rename (v1 protocol/default decoding unchanged). Keep the cursor schema-ID change for 8d3c. Blueprint catalogue summary needs **582** distinct native cases instead of **579**; the total and dispositions remain unchanged. No lead-owned page was edited.

8d3b deletes the retained `database/query` package (**195 production Go files and 97 test files**) and the 13 remaining bridge files or legacy sections (preserving the native sections in mixed files), indirect extension wrappers, compatibility declarations, and the 8d1 guard allowlist. Retire the one-time `tools/goway-retire` codemod after its migrations are integrated. Install the decision-7 whole-tree absence guard and its negatives; do not exempt archived source as executable Go. Retain the shared compilers, native capability bridges, historical cursor-identity fixture, frozen evidence and named assertion owners. The declaration/test ledger is the deletion checklist, not permission to discard tests by filename alone.

## Final ledger guard correction

Replacement entries must name `_test.go` files selected by the current Go build context, outside `testdata` and ignored directories. The parsed declaration must be a top-level, non-generic Test/Benchmark/Fuzz entry point with one pointer parameter from the actual `testing` import (`T`, `B` or `F` respectively), no results and a body. Go's test-name boundary is checked; a prefix alone is insufficient. Import aliases are resolved from parsed imports.

Regressions reject archived `.go.txt`, build-ignored files, testdata, lowercase prefix helpers, missing/wrong parameters, a foreign package aliased as testing, and result-bearing functions. The positive fixture now has an executable test signature; a separate matrix covers valid aliased Test/Benchmark/Fuzz signatures and rejects generics, value parameters and multiple parameters.

Guard tests and root `make test` restricted to `tools/goway-retire` pass on r19. Docs and formatting checks pass. No runtime, ledger disposition, footprint or frozen-record change.
