# Stage 8c implementation handoff

Base: `545081856a448724eef810bf5e1e8ea877abcb84`. No commit. Runtime retirement remains Stage 8d.

## Urgent replay fix

`/private/tmp/goway-freeze-consumption-fix.patch` contains only four harness files relative to the freeze commit. Expected observations use the reviewed PostgreSQL census and the same URL/REQUIRED enablement constants as testkit. Disabled backend entry points and explicitly skipped descendants are deselected; enabled entry points retain strict occurrence completeness. Pending observation overrides do not redirect the source census. Regression sources cover disabled, URL-enabled, REQUIRED-enabled, explicit skip, partial consumption and pending capture modes.

## Surgery

The following generator implementation files are retired:

- `internal/generate/discover_extensions.go`
- `internal/generate/discover_field_methods.go`
- `internal/generate/discover_hooks.go`
- `internal/generate/discover_model_scopes.go`
- `internal/generate/discover_projection.go`
- `internal/generate/discover_soft_deletes.go`
- `internal/generate/discover_timestamps.go`
- `internal/generate/emit_model_aggregates.go`
- `internal/generate/emit_model_audit.go`
- `internal/generate/emit_model_changes.go`
- `internal/generate/emit_model_each.go`
- `internal/generate/emit_model_extensions.go`
- `internal/generate/emit_model_fields.go`
- `internal/generate/emit_model_hooks.go`
- `internal/generate/emit_model_insert_select.go`
- `internal/generate/emit_model_locking.go`
- `internal/generate/emit_model_lookup.go`
- `internal/generate/emit_model_mutation.go`
- `internal/generate/emit_model_ownership.go`
- `internal/generate/emit_model_query.go`
- `internal/generate/emit_model_reference.go`
- `internal/generate/emit_model_relations.go`
- `internal/generate/emit_model_retrieval_hooks.go`
- `internal/generate/emit_model_set_writes.go`
- `internal/generate/emit_model_source_write.go`
- `internal/generate/emit_mutation_input.go`
- `internal/generate/emit_projection.go`
- `internal/generate/model_declarations.go`
- `internal/generate/projection_variants.go`

Model/projection dispatch, metadata and declaration dependencies are removed from discover.go, emit.go and declarations.go. load.go retains general overlays; graph.go loses only model table-name checks. generated_names.go remains the generic collision owner. emit_json_paths.go retains DTO path naming. Native persistence recognition uses checked Table/Builder type arguments, including imported packages. The preliminary table namespace exposes its model-bearing handle; the full overlay still checks every original capability field and initializer. Enumeration shares the export importer lock.

DTO, generic DTO, validation, enum, config, message, multipart, union, HTTP path and query-string emission remains. Enum codecs and native scaffold slot descriptions retain one owner. Publication, ownership, locks and recovery remain; field-documentation publication only cleans obsolete managed notices.

## Deleted test replacement ledger

Paths without a prefix below are under tests/fixtures/goway. These are the retained coverage sources; command outcomes and remaining limits are recorded below. Model-emitter-specific syntax/collision tests retire with the API; TestRetiredPersistenceDirectivesAreActionable rejects its annotations. Retained DTO/config/enum collision and publication tests remain.

| Deleted internal/generate test | Replacement coverage |
| --- | --- |
| `aggregates_test.go` | relation_aggregates_golden_test.go (TestRelationAggregateGoldens), aggregate_golden_test.go |
| `binary_model_test.go` | lifecycle_golden_test.go; database/orm/codec_surface_test.go; typed column compile-fail cases |
| `closure_guard_test.go` | footprint_test.go; database/orm/method_surface_test.go and phase_substitution_test.go |
| `encrypted_model_test.go` | sensitive_golden_test.go; database/orm/sensitive_declaration_test.go |
| `extensions_test.go` | extension_handles_test.go, extension_handles_behavior_test.go; internal/generate/scaffold_model_test.go |
| `interval_test.go` | scalar_golden_test.go (TestStage6d2ScalarGolden); pgsmoke/scalars_test.go |
| `join_test.go` | joins_golden_test.go, on_conditions_golden_test.go; Stage 6b compile-fail cases |
| `model_audit_test.go` | audit_golden_test.go (TestAuditOldAndNewAgree); internal/generate/generate_diagnostics_test.go for retained symbol collisions |
| `model_changes_test.go` | lifecycle_golden_test.go; database/orm/lifecycle_failure_test.go |
| `model_each_test.go` | store_capabilities_golden_test.go (TestStoreEachLifecycleGolden) |
| `model_global_scopes_test.go` | retrieval_scope_golden_test.go; consumer/officequeries/desk_scope_postgres_test.go |
| `model_hooks_test.go` | lifecycle_golden_test.go; consumer/pivothooks/pivot_hooks_postgres_test.go |
| `model_insert_select_test.go` | source_write_golden_test.go (TestSourceWriteOldAndNewAgree) |
| `model_lookup_test.go` | terminal_writes_golden_test.go (TestTerminalWritesGolden) |
| `model_reference_test.go` | models/identity/models_test.go; consumer/reporting/orm_test.go |
| `model_retrieval_hooks_test.go` | retrieval_scope_golden_test.go; read_terminal_golden_test.go |
| `model_source_write_test.go` | source_write_golden_test.go and source_write_test.go |
| `mutation_input_test.go` | mutators_golden_test.go; json_path_golden_test.go; typed-role compile-fail cases |
| `mutators_test.go` | mutators_golden_test.go (TestMutatorsOldAndNewAgree, TestInputPrimaryKeyOldAndNewAgree); database/orm/mutator_test.go |
| `password_model_test.go` | sensitive_golden_test.go (TestPasswordOldAndNewAgree); database/orm/password_mutator_test.go |
| `projection_dto_test.go` | report_projection_golden_test.go; internal/generate/dto_test.go and persistence_models_test.go |
| `projection_test.go` | records_golden_test.go, result_read_golden_test.go; Stage 6c compile-fail cases |
| `relation_writes_test.go` | relation_writes_test.go (TestRelationWriteGoldens); typed-role compile-fail cases |
| `relations_test.go` | relations_golden_test.go, relations_joined_golden_test.go; Stage 4 compile-fail cases |
| `soft_deletes_test.go` | lifecycle_golden_test.go; retrieval_scope_golden_test.go; typed soft-delete role compile-fail cases |
| `timestamps_test.go` | lifecycle_golden_test.go; database/orm/mutator_test.go; typed timestamp role compile-fail cases |

## Ownership and callers

`make generate` removed 49 consumer and 28 goway obsolete owned outputs. The eight oldmodels/oldreporting package manifests were empty after publication; seven package directories retired, and the root oldmodels path now contains only the approved handwritten cursor identity fixture. Retained mixed-package manifests shed only model/projection outputs. Models, mutatorqueries, reporting, jsonqueries and other enum/DTO directories keep their remaining manifests. No retained generated output or protected contract file has a content diff. The verification loop checks contract byte identity against the pre-surgery hashes.

All 909 frozen observations and their publication receipt remain untouched. Recording CLI, live legacy adapters and generated oldmodels/oldreporting packages retire. New API workloads retain stable test/site identities and compare only to frozen observations. The import guard has no recording-file or migration-before exception. Migration before blocks are documentation-only; after blocks retain compiled coverage using native report declarations.

The 148 generated-model compiler originals remain unchanged text inputs (the inventory listed 140; compilation identified eight additional pre-port originals with existing ORM counterparts). A hash-bearing retirement registry excludes precisely those cases from compiler batches; the catalogue still checks their ported counterparts. Historical generated exports live in that registry so removing emitters cannot weaken classification. Handwritten legacy runtime probes remain until Stage 8d.

Synthetic workloads now use native declarations only; old measurements remain historical. Release inventory expects frozen records instead of the recorder and old packages. Editor anchors in measure-release.py point to native productionprofile code. internal/agent remains lane D-owned and was not edited.

## Initial Stage 8c acceptance

The approved handwritten historical-identity fixture, `oldmodels/cursor_identity.go`, retains `oldmodels.User` and binds it with ORM; current enum codecs are shared. The import guard permits only reads_golden_test.go to import this exact package. It rejects old subpackages, oldreporting, other importers, and additional source/generated code in the historical fixture. Frozen tokens and comparisons remain unchanged.

Completed checks were retained across the r14-to-r15 rotation. The interrupted consumer race command alone was restarted on r15. Every subsequent Go command used GOCACHE=/private/tmp/foundry-goway-codex-gocache-r15, GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off. The lead clarified that acceptance remains required; the cache rotation only prevents repeating already-passed checks. Completed results were retained and outstanding checks resumed on r15.

| Command / segment | Result |
| --- | --- |
| `go build ./...`; `go vet ./...` | Pass; final r15 logs in /private/tmp/8c-build-vet-final.log |
| `go test -race -timeout=20m ./internal/generate ./tests/oracle` | Oracle passed; three generator fixture failures were corrected and affected race tests passed in subsequent segments |
| Final affected generator and cmd/foundry races | Pass on r15; /private/tmp/8c-generator-final-r15.log |
| Fixture vet/tests (plugins, consumer, goway) | Pass in segments; corrected fixture failures and stale legacy-count assertion passed in the affected reruns |
| Goway races, including full root replay and footprint probes | Pass; /private/tmp/8c-goway-race.log |
| Consumer races, 32 touched packages | Pass after interrupted command restarted on r15; /private/tmp/8c-consumer-race-r15.log |
| `python3 tools/goway_catalogue.py` | Pass; /private/tmp/8c-catalogue-final.log |
| `make generate` | Pass; final regeneration after handwritten hover comments changed no outputs |
| `make generate-check` | Pass on final source, r15; /private/tmp/8c-release-generation-r15.log |
| `make typescript-check` | Pass with required native Node/TypeScript tools |
| `make release-tools-check` | Pass: Go vet/test and Python 29/29. Sandbox process-inspection restrictions required a native rerun; /private/tmp/8c-release-generation-r15.log |
| `make docs-check` | Pass after applying the approved 27-link historical-path repair |
| `make agent-smoke` | Pass in segments: all unchanged probes passed in the full run; the three repaired raw-field probes passed on r15 using the target’s test command narrowed to those subtests. /private/tmp/8c-agent-affected-r15.log |
| Formatting | Pass: make fmt-check on r15 |
| `cd tools/release && go test -race -timeout=20m ./...` | Pass on r15; /private/tmp/8c-release-race-r15.log |

No live PostgreSQL was run; PostgreSQL sources compiled and backend-disabled tests skipped. Earlier byte audits confirmed all 166 retained generated outputs, all 45 protected contract files, and all 909 frozen observations unchanged. No public ORM method additions or existing footprint ceiling changes; all footprint probes passed.

The retirement registry contains 148 immutable compiler originals and 147 ORM counterparts. Eight additional already-ported originals found beyond the inventory's 140 were nonnullable_clear, draft_value, batch_result_owner, batch_draft_owner, set_record_owner, set_input_scope, set_output_scope and model_change_ignored_field.

The approved patch `/private/tmp/goway-8c-doc-links.patch` was applied and replaces only broken links with labelled historical paths in blueprint/05-typed-model-and-query-core.md, blueprint/06-relations-and-advanced-queries.md, blueprint/goway-data-layer/README.md and blueprint/model-extension-slots/01-slot-declarations-and-generation.md. The lead authorized this narrow exception to blueprint ownership. Broader historical prose remains owned by Stage 10.

Initial Stage 8c non-PostgreSQL acceptance completed before review 38. No commit was made. The affected editor rerun used `go test -timeout=20m -count=1 -v ./internal/agent -run '^TestRealGoplsFieldBehaviorDocumentation$/(raw-field-getter|raw-field-setter|raw-field-both)$'` with the approved FOUNDRY_TEST_GOPLS path. Previously passed editor probes were not repeated.

## Review 38 corrections

Persistence recognition now traverses slice elements, array elements, and both map keys and values. It also resolves actual ORM Define instantiations through go/types objects and type arguments; no selector-name heuristic grants persistence status. The complete checked-package graph retains these model identities for dependent source packages, including tables erased behind interface containers.

Declaration dependency reachability is shared by both passes. Annotated roots and all their transitive dependencies retain complete shapes before schema discovery. Only table namespaces outside that graph defer column capabilities that require generated enum methods. Nested transport structs therefore keep supported fields and reject unsupported ones.

Fresh-checkout regressions cover seven declaration shapes, each as local and imported models used as DTO fields, embeddings and map keys (42 negatives). The nested DTO regression checks Label in generated contract output, repeat-generation currency, and rejection of Callback func() without publishing output. Existing enum namespace regressions remain.

Shared files: internal/generate/declarations.go, load.go, graph.go, persistence_models.go and persistence_models_test.go. No public API, dependency, contract or footprint ceiling changes.

Review acceptance used GOCACHE=/private/tmp/foundry-goway-codex-gocache-r16, GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.

| Check | Result |
| --- | --- |
| `go test -race -timeout=20m ./internal/generate` | Pass, full suite including all new regressions; /private/tmp/8c-review38-generator-race-r16.log |
| `make generate-check` | Pass; all 166 retained generated outputs match both the pre-review snapshot and pre-surgery baseline byte-for-byte |
| `make fixture-check` | Pass: vet/tests for plugin_base, plugin_dep, consumer and goway, including replay, import guard and footprint probes; /private/tmp/8c-review38-fixtures-r16.log |
| `python3 tools/goway_catalogue.py` | Pass: 1,032 rows, 587 ported |
| `make fmt-check` | Pass |
| `make docs-check` | Pass; /private/tmp/8c-review38-docs-r16.log |

No live PostgreSQL was run. No commit was made.

## Review round 3

Persistence model recognition traversal in `internal/generate/persistence_models.go` is closed for all container and declaration shapes:
- **Generic type arguments:** In the `*types.Named` case, visits `typ.TypeArgs().At(i)` before same-package checking, recognizing persistence models passed as type arguments to generic containers declared in external packages (`lib.Box[store.Table[Account,int]]`).
- **Interfaces:** Added `case *types.Interface` visiting explicit method signatures (`typ.ExplicitMethod(i).Type()`) and embedded types (`typ.EmbeddedType(i)`), recognizing table handles returned by interface methods (`Repo interface{ Accounts() store.Table[Account,int] }`).
- **Methods on named types:** For same-package `*types.Named`, visits each method signature (`typ.Method(i).Type()`), capturing table handles returned from methods on structs or other defined types.
- **Channels:** Added `case *types.Chan` visiting element type (`typ.Elem()`), recognizing channels conveying table handles.
- **Erased/wrapper limits documented:** Documented the recognition boundary where `any`-erased `orm.Define` initializers require source-checked packages (`Info.Instances`), as compiled Go export data omits expression initializers, and custom user wrappers around `orm.Define` cannot be tracked if erased to untyped interfaces.
- **Preliminary table shape clarification:** Added comment to `nativeTableShape` explaining that named `Table[...]` fields outside the transport graph are intentionally dropped from preliminary shapes to isolate column capability analysis.

The negative test matrix in `internal/generate/persistence_models_test.go` was extended with 7 new shapes (`ptr_slice`, `generic_wrapper`, `iface`, `method`, `chan`, `func_result`, `alias`) across both local and imported packages, testing all 3 DTO contexts (field, embedded, map key) for 42 new regressions (84 total). Each was verified to fail when its corresponding fix was reverted.

## Review round 4

Closed cross-package traversal poisoning and type-parameter constraint recognition in `internal/generate/persistence_models.go`:
- **Cross-package `seen` isolation:** `seen` tracking is scoped per package in `visitPackage`, and for `*types.Named`, `seen[typ]` is recorded only when walked within its defining package (`typ.Obj().Pkg() == pkg`). This prevents foreign non-generic structs (such as `lib.Holder` containing `store.Table`) and foreign methods from poisoning `seen` before their defining package is walked.
- **Type-parameter constraints and signatures:** Added `case *types.TypeParam` visiting `typ.Constraint()`, walked `TypeParams()` of Named origins (bounded by `seenTypeParams[typ.Origin()]`) and of Signatures, and visited Signature receivers (`typ.Recv().Type()`).
- **Recursive structures:** Verified that self-referential structures (`type Node struct{ Next *Node; T store.Table[...] }`) and recursive interfaces (`type I interface{ M() I; T() store.Table[...] }`) terminate cleanly while registering persistence models.
- **Extended negative tests:** Added `lib_holder`, `cross_pkg_method`, `constraint`, `recursive_node`, and `recursive_iface` across local and imported packages and all 3 DTO positions in `internal/generate/persistence_models_test.go` (30 new tests, 114 total). Confirmed that each new escape shape fails when its fix is reverted.
- **Documentation:** Updated `docs/guides/http-dtos.md` to state exact recognition coverage across containers, cross-package methods, constraints, and recursive shapes.

## Review round 5

Resolved robustness issues with generic type expansion, exponential shared type-argument DAGs, constraint unions, and generic aliases in `internal/generate/persistence_models.go`:
- **Expanding generic types probe & handling:** Probed `type N[T any] struct{ next *N[[]T]; t store.Table[Account,int] }` with `go/types`. The Go compiler rejects expanding recursive generic types at type-check time with an instantiation cycle error (`instantiation cycle: T instantiated as []T`). Per instructions, this test was skipped and noted. In `persistence_models.go`, origin walks are keyed on `seenOrigins[typ.Origin()]` and traverse `typ.Origin().Underlying()`, decoupling origin definition traversal from instance type arguments to ensure termination.
- **Exponential shared type-argument DAG pruning:** Moved the package-scoped `seen[typ]` check and set to the top of `case *types.Named:` immediately following the Table/Builder return. This guarantees linear $O(N)$ traversal across generic DAG chains (`Pair[DAG_{k-1}, DAG_{k-1}]`) instead of $O(2^N)$ exponential branching, while remaining strictly package-scoped to avoid cross-package poisoning. Added a 30-level DAG test shape (`dag_chain`) that traverses in 30 visits and completes in ~0.3s.
- **Constraint unions:** Added `case *types.Union:` visiting each term's type `typ.Term(i).Type()`. Tested with `type C interface{ ~[]store.Table[Account,int] }; type Box[T C] struct{}` (`constraint_union`).
- **Generic type alias parameter constraints:** Prior to `types.Unalias(typ)`, when `typ` is `*types.Alias`, walked its `TypeParams()` constraints (guarded by `seenAliasTypeParams[alias.Origin()]`) and `TypeArgs()`. Tested with `type A[T interface{ Get() store.Table[Account,int] }] = []T` (`generic_alias`).
- **Round-4 mechanism coverage:** Added regression shapes for generic function constraints returning table handles (`func F[T interface{ Get() store.Table[Account,int] }]() {}`, resolving empty block body requirement for generic functions in `declarations.go`) and cross-package generic type instantiation with a named interface constraint (`var B lib.Box2[lib.Getter]`).
- **Reversion confirmation:** Reverted each fix individually (`constraint_union`, `generic_alias`, `func_constraint`) and confirmed negative test failures before restoring.
- **Documentation:** Updated `docs/guides/http-dtos.md` to document struct fields, anonymous structs, func values, function-body boundary limit, constraint unions, and generic type aliases.

## Review round 6

Closed persistence-model recognition for generic instances and enforced fail-closed traversal completeness in `internal/generate/persistence_models.go`:
- **Generic instance substitution:** Generic type instantiations (`typ.TypeArgs().Len() > 0`) now traverse the instance's own substituted underlying type (`typ.Underlying()`) and methods (`typ.Method(i).Type()`) across all packages (both local and foreign/imported), guarded by `seen[typ]` on the instantiated `*types.Named` pointer. For non-instances, origin-based expansion (`typ.Origin().Underlying()`) remains restricted to defining packages (`typ.Obj().Pkg() == pkg`) guarded by `seenOrigins[typ.Origin()]`, preserving termination and avoiding cross-package poisoning.
- **Fail-closed exhaustive switch:** The type visit switch in `persistenceModels` explicitly lists all 14 standard `go/types.Type` implementations (`*types.Named`, `*types.TypeParam`, `*types.Basic`, `*types.Alias`, `*types.Pointer`, `*types.Slice`, `*types.Array`, `*types.Chan`, `*types.Map`, `*types.Struct`, `*types.Interface`, `*types.Union`, `*types.Signature`, `*types.Tuple`). A `default:` arm fails closed with `failure = fmt.Errorf("unhandled type kind: %T", typ)` and immediately aborts traversal. Tested via unit test `TestPersistenceModelsFailsClosedOnUnhandledTypeKind` using a custom `types.Type` implementation.
- **AST declaration retention:** Updated `nativeDeclaration` in `declarations.go` to retain `*ast.IndexExpr` and `*ast.IndexListExpr` variable declarations (`var H GenericHolder[Account]`) during declaration file filtering so generic instantiations are preserved for type checking.
- **Declarations cleanup:** Reverted method branch in `declarations.go` to `copy.Body = nil` because Go methods cannot declare type parameters.
- **Extended negative tests:** Added shapes for local and imported generic structs (`generic_holder`, `lib_generic_holder`), local and imported generic method receivers (`generic_method`, `lib_generic_method`), and generic type alias origin reuse (`generic_alias_twice`) across all 3 DTO contexts (field, embedded, map key) for 24 new regression tests (138 total in `TestNativePersistenceModelsCannotBecomeTransportDTOs`). Reverting the instance substitution confirmed all new shapes fail before restoring.
- **Documentation:** Updated `docs/guides/http-dtos.md` to document generic instance substitution coverage and fail-closed traversal on unknown types.

## Docs notes

Lead-owned changelog/master/blueprint updates: model/query/projection generation is retired; persistence uses handwritten ORM declarations; transport generation remains. Legacy parity is frozen historical evidence. Before migration examples are documentation-only; after examples remain compiled. PostgreSQL-only oracle completeness follows enabled acceptance. The historical cursor identity exception preserves token compatibility during a real handwritten migration.

## Review round 7 — redesign

Review round 7 showed that recognition from the declaration pass was unsound by construction. That pass is a syntactic subset checked with `IgnoreFuncBodies`, and its `Define` set was merged in worker-completion order. Generic instances outside an explicit `var` type escaped (P1). Named table fields were dropped (P2-a). The orm-specific selection broke valid packages (P2-b), and sibling recognition was nondeterministic (P2-c). Recognition was redesigned in `internal/generate/persistence_models.go`:

- **Authoritative check after generation:** each package whose imports reach `database/orm` (from `go list` `Deps`) keeps its complete overlay check. It records the distinct types from that check's `Types`, `Defs` and `Uses`, plus its `Instances` (`persistenceSource`). The check includes function bodies and initializers. After every worker finishes, one sequential structural walk runs over those sources. It also covers the package scopes of the loaded export-data dependencies that import orm. The walk records the model argument of every `Table`/`Builder` handle and every `Define`/`TableOf` instantiation. Substituted generic instances are walked with their fields and methods. The round-6 exhaustive kind switch still fails closed.
- **Two phases, deterministic:** `prepare` builds the run-wide model set only after every package passes, and then validates every DTO, union and multipart graph against it. Models are keyed by package path and type name, so declaration-pass, complete-check and export-data objects agree. A violation fails generation before anything is published.
- **Preliminary pass:** `nativeDeclaration`, `nativeTableShape` and the orm-specific declaration and method selection are removed. So are the declaration-pass `types.Info`, `graph.definedModels`, `definedPersistenceModels` and the per-package closure walk held under `compiledMu`. Discovery keeps only the type-local `FoundryIdentity` check.
- **Discovery failure refinement:** a struct model used as a map key, or a model with fields DTOs cannot encode, fails discovery before the run-wide check. The reported failure then gets a best-effort recognition. That recognition uses the failed package's own handwritten files, checked tolerantly because its output does not exist yet, plus its completed import closure. If the failure's partial graph reaches a recognized model, the persistence diagnostic is reported in its place. The refinement only rewrites an existing failure and depends on no unrelated package. A model declared only by an unrelated sibling keeps the original discovery diagnostic.
- **Diagnostics:** recognition errors carry the position, package and object (P3-a). A model argument that is not a named type, such as `Table[*Account,int]`, an anonymous struct or `Builder[[]Account]`, fails with a positioned diagnostic (P3-e). Instances are deduplicated by origin pointer and type string (P3-b).
- **Performance:** packages without a `database/orm` dependency are neither recorded nor walked (P3-c). The walk uses one memo for the whole run and takes `compiledMu` only around each export-data import. `make generate-check` covered all 15 generation roots in 6.6 s wall time on the warm r16 cache.
- **Partial runs:** a `--dir` run recognizes models from its selected packages and their import closure. The full `make generate`/`make generate-check` runs are the authoritative gate; see `docs/guides/http-dtos.md`.

The negative matrix now has 36 shapes × local/imported × field/embedded/map key (216 subtests). The new shapes are seven: `generic_initializer`, `generic_function_body`, `generic_struct_field`, `generic_result`, `wrapper_function`, `generic_alias_handle` and `named_table_field`. Five new tests were added:

- `TestPersistenceModelsFromSiblingPackagesAreDeterministic`: the P2-c layout, five full runs per DTO context, with identical diagnostics.
- `TestPersistenceRecognitionKeepsValidPackagesGenerating`: the P2-b packages, with fresh and current-output checks.
- `TestPersistenceModelArgumentsMustBeNamed`.
- `TestPersistenceModelDiagnosticPrecedesItsShape`.
- The positioned fail-closed unit test, which covers both recognition inputs.

Reversion evidence:

- On the pre-redesign source, the local variants of all seven new shapes failed; the imported variants were already rejected, apart from `generic_function_body`. The sibling test's field and embedded variants failed, and so did every P2-b, unnamed-argument and precedence case. The sibling map-key variant asserts only a deterministic map-key rejection, and it passed on the old source too.
- With the refinement disabled, the precedence test and every model map-key negative failed.

| Check | Result |
| --- | --- |
| `go build ./...`, `go vet ./internal/generate/...` | Pass |
| `go test -race -timeout=25m ./internal/generate/...` | Pass (312 s) |
| `make generate-check` | Pass; all 15 generation roots current, so generated output is byte-identical |
| `go test -run '^TestInvalidPublicContractsFailCompilation$' .` in `tests/fixtures/goway` | Pass |
| `make fmt-check`, `make docs-check` | Pass |

All checks used GOCACHE=/private/tmp/foundry-goway-codex-gocache-r16, GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off. No PostgreSQL run was needed, and no commit was made.

## Review round 8

- **Void generic wrappers (P2):** a persistence generic whose model argument is a type parameter was skipped on the assumption that the instantiating site exposes it. That assumption fails when the wrapper's instances carry no handle, for example `func Register[M any]() { orm.Define[M](...) }` or a method `func (h Holder[M]) Init()` that declares a table for `M`.
  - `collectPersistenceSource` now maps each type parameter a package declares to its owner and index. A function's own parameters belong to the function; a method's receiver parameters belong to the receiver's generic type; type and alias parameters belong to their declarations.
  - `resolveBearers` computes, to a fixpoint over the run's source instances, every generic that passes a type parameter to a persistence generic or to another such generic.
  - Every in-run instance of a bearer records its type argument, whether found through `Instances` or a walked `Named`/`Alias` origin. Non-named arguments fail closed as before.
  - Four matrix shapes were added (24 subtests, 240 in total): `void_generic_function`, `void_generic_method`, `void_wrapper_chain` (outer wrapper declared first) and `lib_void_wrapper` (wrapper in another run package). All 24 failed with bearer resolution removed, and the chain shape also failed with a single resolution pass.
- **Deterministic ordering (P3):** instance and root sorts are stable and break remaining ties on the type string.
- **Refined diagnostics (P3):** a refined discovery failure appends the original generator diagnostic, `(discovery also failed: ...)`. That text is the diagnostic the run would otherwise report. `TestPersistenceModelDiagnosticPrecedesItsShape` asserts both parts.
- **Docs (P3):** `docs/guides/http-dtos.md` lists wrapper resolution and the export-data limits for wrappers and unexported declarations. A probe confirmed the unexported limit: export data omitted an unexported variable and type, but kept an unexported variable that an inlinable exported function referenced.

Verification used the round-7 environment:

| Check | Result |
| --- | --- |
| `go build ./...`, `go vet ./internal/generate/...` | Pass |
| `go test -race -timeout=25m ./internal/generate/...` | Pass (326 s) |
| `make generate-check` | Pass; all 15 generation roots current (6.5 s) |
| `go test -run '^TestInvalidPublicContractsFailCompilation$' .` in `tests/fixtures/goway` | Pass |
| `make fmt-check`, `make docs-check` | Pass |

Round 8 follow-up:

- A wrapper (bearer) instantiated with a basic or interface argument (`int`, `any`) records nothing. A non-named composite argument (`*Account`, `[]Account`) still fails closed with "must be a named struct type". The direct `Table`/`Builder`/`Define`/`TableOf` instantiations keep failing for any non-named argument.
- The header comment names `resolveBearers`. The guide now states wrapper coverage as any mention of a type parameter as the model argument of a persistence generic or wrapper, regardless of control flow. Its limits add three items: a wrapper's non-named composite arguments fail, while basic and interface arguments are ignored; wrapper resolution covers only packages in the run; and a refined diagnostic appends `(discovery also failed: …)`.
- Five new matrix shapes (30 subtests, 270 in total): `void_define_wrapper` (a real `Define`/`TableOf` call), `inferred_wrapper`, `alias_wrapper`, `lib_wrapper_chain` and `permuted_wrapper` (`Wrap[D, M]` embedding `Repo[M]`). New positives: `second_parameter_not_model`, in which `Service[Account, Summary]` leaves `Summary` a valid DTO, and `wrapper_basic_interface_arguments`. New fail-closed cases: `wrapper_pointer` and `wrapper_slice`.
- With bearer resolution removed, all 30 new matrix subtests failed. Without the relaxation, the basic/interface positive failed. With an index-blind `bearing`, the second-parameter positive failed.
