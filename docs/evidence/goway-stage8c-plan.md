# Stage 8c: generator surgery execution plan

Planning snapshot, 2026-10-25: laneA9 at `fdaff938`, including its accepted,
uncommitted Stage 9 delta through LoadFrom/LoadMissingFrom. Batch 7e and lane D's
nestedbindings migration have not landed in this checkout. This document is the
only edit for this planning turn; no generation or verification was run.

Authority: PLAN §4.9 and Stage 8 in `proposals/foundry-go-goway-data-layer/PLAN.md`,
lane D's inventory in `proposals/foundry-go-goway-data-layer/patches/stage8-inventory.md`,
and Stage 9 decisions in `proposals/foundry-go-goway-data-layer/patches/stage9-decisions.md`
(maintainer workspace, outside this repository).
The inventory describes an older checkout. In this tree the framework stores
already have ordinary declarations, the model scaffold already renders ORM, and
the shared runtime contracts have been extracted. Do not repeat those migrations.
Stage 8c retires maintained model/projection generation; retiring the legacy query
runtime and parity oracles is Stage 8d, under a separate lead decision.

## 1. Entry conditions and unresolved decisions

1. Integrate the remaining Stage 9 packages, negatives, editor probes and accepted
   deltas. Resolve the blockers in section 5 before removing their generated APIs.
2. Owners remove handwritten coexistence factories and model/projection directives
   at the serial cleanup barrier. Keep model structs, IDs, enums and relation slots
   still used by ORM. Convert any remaining `projection dto=true` declaration to
   `dto` plus its handwritten rows declaration before removing projection discovery.
3. **Oracle publication decision:** decision 5 preserves `goway/oldmodels` and
   old/new goldens until 8d. They still have model directives and lie inside the
   recursive `tests/fixtures/goway` generation root. Simply removing dispatch either
   rejects them or makes their outputs obsolete. The lead must approve a specific
   frozen-evidence publication boundary (including `oldreporting`) or coordinate
   their separately authorized retirement. Do not silently skip these packages in
   build/tests, alter hashes, or keep a second hidden model generator.
4. **JSON-path contract decision:** §4.9 explicitly retains `emit_json_paths`.
   Today `emitJSONField` is reached only from `emitScopedFields`, and its emitted
   paths embed legacy query descriptors. `jsonPathName` is also used by retained DTO
   validation. Preserve the file, schema/name/documentation semantics and contract
   tests; settle the retained declaration/emission entry point before disconnecting
   its only producer. Do not invent an annotation or pretend this is already an
   independent DTO emitter. C owns the ORM JSON expression surface; lead owns the
   generator contract decision. Model-specific wrappers can then be removed while
   approved contract/path generation remains reachable.
5. **Persistence/DTO separation:** `discover.go` supplies a same-package model set
   to `resolveDTOSchema`, `resolveJSONGraph`, `resolveMultipartFields` and map-key
   validation. Deleting it must not silently allow persistence models as DTO fields,
   embeddings or map keys. Retain existing identity/contract checks; establish the
   native declaration recognition policy and corresponding fresh-checkout negative
   before deleting that set. An ordinary ORM model need not have the generated
   `FoundryIdentity` method, so that method alone is not an equivalent replacement.

### Migration guide before/after examples (lead decision after batch 7g)

`tests/fixtures/goway/migration/{guide,examples,sql}_test.go` are the exact
**migration-before examples** exception in the oracle import guard. They currently
compile the guide's old-API snippets intentionally. During 8c, move the **before**
blocks to documentation-only, uncompiled examples; keep the **after** blocks compiled
against ORM. Update the guide snippet check to enforce that distinction, remove the
three-file exception once generated imports are gone, and retain the recorded
`TestDeclarationsAndReadSQL` observations as the legacy SQL reference. Do not retain
a hidden legacy generator or permit other files to borrow this exception.

## 2. Exact source surgery

Paths in this section are relative to `internal/generate/`.

Delete these files after extracting the shared helper noted below:

```text
emit_model_aggregates.go          emit_model_audit.go
emit_model_changes.go             emit_model_each.go
emit_model_extensions.go          emit_model_fields.go
emit_model_hooks.go               emit_model_insert_select.go
emit_model_locking.go             emit_model_lookup.go
emit_model_mutation.go            emit_model_ownership.go
emit_model_query.go               emit_model_reference.go
emit_model_relations.go           emit_model_retrieval_hooks.go
emit_model_set_writes.go          emit_model_source_write.go
emit_mutation_input.go            emit_projection.go
discover_extensions.go           discover_field_methods.go
discover_hooks.go                discover_model_scopes.go
discover_soft_deletes.go          discover_timestamps.go
discover_projection.go           model_declarations.go
projection_variants.go
```

`emit_model_query.go:fieldCodec` also supplies `emitEnum` and JSON-path scalar
codecs. Move that single implementation to a retained codec-emission owner before
deleting the file; retain its supporting field/scalar metadata as needed. Do not
copy it into each emitter. Extension contract inference in
`discover_extensions.go` serves generated extension registration; native scaffolds
already supply their own explicit metadata contracts. Preserve DTO discovery itself.

| File | Surgical change / retained responsibility |
| --- | --- |
| `discover.go` | Remove `model`, `relationField`, `aggregateField`, `metadata.models`, `metadata.projections`; model/projection dispatch, model table/extension-owner validation and generated-name reservations, model/projection enum/path resolution and sorting, `discoverModel`, `discoverValueField`, `checkEncryptedFields`, and model-only `fieldTag`. Remove `inferMetadataContracts` invocation. Remove accepted `model`/`projection` directive kinds, with an actionable retirement diagnostic rather than silently ignoring them. Resolve section 1's DTO model-set issue first. Keep enum/transport/config/union/DTO resolution, `directive`, diagnostics, `parseTags`, `snake`, `exportedName`, scalar classification/enum helpers required by retained codecs and paths. Trim `field` only after those uses are separated. |
| `emit.go` | Remove model loops, package `FoundryExtensions` emission, projection/DTO merge loop, `emitModel`, `fieldClass`, and projected-DTO skip. Keep emitter/import/local-name machinery, formatting/output collision detection, `emitEnum`/`integerBits` and retained dispatch. |
| `declarations.go` | Remove `modelFieldMethods`, `fieldMethodPrefix` handling, model-only global-scope factory reachability, persisted-struct substitution and `persistedDeclaration` (then its unused import). Keep declaration dependency reachability, const/iota/enum aliases, generic type parameters, union stubs, codec/contract method signatures, handwritten name/method recording and complete-overlay validation. Do not drop identity recognition until the DTO boundary is resolved. |
| `generated_names.go` | **No model-specific branch remains in this version.** Keep `checkGeneratedNames`, `jsonDescriptorNames`, `validationDescriptorNames` unchanged. Model name reservations actually live in `discover.go`; do not delete general collision checks to satisfy the old inventory's filename list. |
| `load.go` | **No hard-coded model/projection dispatcher exists here.** Keep `packageInput`, `loadPackage`, `check`, `mayDeclare`, `hasDeclarationDirective`, previous-output tracking and declaration-free full-overlay checking. Update comments if needed; no wholesale loader removal. Keep unsupported directives discoverable so their migration diagnostic runs. |
| `graph.go` | In `prepare`, remove the `tables` map and cross-package `metadata.models` duplicate-table loop. Keep message-key uniqueness, package ordering/import/export-data handling, overlays, bounded preparation, build-constraint/cgo checks, module/source snapshots and ownership publication. Update the model-only `fieldDocumentation` comment; retain cleanup/recovery reachability for formerly generated packages. |
| `discover_dto.go` | Remove `dtoDeclaration.projected`; resolve model-set dependencies across DTO/union/multipart/map-key discovery without weakening the model/transport boundary. Keep all codec, generic, validation and wire identity paths. |
| `emit_json_paths.go` | Preserve `jsonPathName`, property discovery, recursion/resource bounds, JSON names, quoted properties, map/array keys, documentation and nullable scalar semantics. Separate `emitJSONField`'s model fields from the approved retained path-contract producer. Retarget SQL descriptor coupling only under the contract decision in section 1, using existing ORM expression owners. |
| `field_documentation.go` | Retire generated model `fieldBehaviorNotes`/`extensionSlotNotes` and the model metadata walk in `planFieldDocumentation`. First remove obsolete managed notes through the existing source-edit publication path, leaving ordinary comments intact. Keep token/comment safety, `stripFieldNotes`, source-edit validation and crash recovery as long as journals/managed notes can exist. |
| `source_documentation.go`, `field_documentation_write.go` | Keep DTO/config/path/validation help and original-source link handling, tracked documentation inputs and publication/recovery. Do not equate removal of model hover notices with removal of all generated documentation. |
| `scaffold.go`, `scaffold_developer.go`, `scaffold_model.go`, `scaffold_model_names.go` | Keep. `renderDeveloperScaffold` already dispatches to `renderORMModel`; model-with-slots generation is for its metadata DTO contract. Remove only obsolete model-generation wording/options after accounting for cleanup recovery. Keep make-model, extension slots and current ORM scaffold goldens. |

Retain `discover_dto.go`, `discover_enum.go`, `discover_config.go`,
`discover_message.go`, `discover_multipart.go`, `discover_union.go`,
`discover_path.go`, `discover_query.go`, `discover_transport_fields.go`;
`emit_dto*.go`, `emit_config.go`, `emit_multipart.go`, `emit_union.go`,
`emit_path.go`, `emit_query.go`, and enum emission in `emit.go`.
`query`/`form` mean HTTP query-string/form generation, not SQL; `union` means a
tagged DTO union. Keep `dto_*` codec/identity/instantiation/map-key helpers,
`json_shape.go`, `url_scalar.go`, presentation and typed `model.ID` support.
Keep `generate.go`, `batch.go`, `artifacts.go`, `module_scope.go`, `write.go`,
`publication_json.go`, `recover.go`, `distribution*.go`, `guard_*.go`, scaffolding
and contract exporters. No runtime `database/query` deletion in this slice.

## 3. Makefile and ownership transition

The lead is the sole publisher. Capture retained wire artifacts before the barrier;
run the existing **`make generate`**, then its `make generate-check` counterpart at
the lead's verification checkpoint. Never hand-delete generated Go, edit manifest
hashes, or regenerate individual fixtures outside the Makefile.

Publication continues through `planWrite`: validate prior ownership/content, remove
only obsolete owned outputs, replace retained outputs, update hashes atomically.
A previously owned directory with no outputs keeps an empty `files` manifest;
a fresh empty directory gets none. Preserve provenance, locks, journals, rollback,
source-documentation recovery and foreign-edit refusal. Cleanup must still reach
directive-free packages. Remove a generation root only after its owned cleanup has
been published; do not remove it first and strand generated files.

Current framework state: all twelve `internal/{attachment,audit,challenge,extension,
idempotency,mfa,notification,outbox,session,token,webhook}store`/`internal/jobarchive`
manifests are already empty. The Makefile still visits some of them; this is harmless
cleanup coverage, not evidence that they still generate models. `countries` retains
its `Status` enum. Keep these generation roots:

```text
logging                         cloud/credentials
infrastructure                  application
internal/authtransport          countries
datatable                       database/orm/internal/enuminit
tests/fixtures/consumer         tests/fixtures/goway (oracle policy first)
```

Within `tests/fixtures/consumer`, the current retained enum/contract directories are:

```text
articles          bootstrap        catalog/status       clientcontracts
configured        genericdto       httpdto              httpkernel
httppagination    httppagination/cmd/pagecontract         httpquery
httpuploads       idempotenthttp   isolatedhttp         localization
mailing           models           multifactor          mutatorqueries
nestedbindings    notifying        outgoing             passwords
profiles          recovering       reporting            requestflow
startupconfig     teamworkflow     unions               validationrules
```

Mixed packages keep their enum/DTO/path/query/form/multipart/message/union/config
outputs while losing only model outputs. `reporting` already declares its result
contracts as DTOs: retain `MemberRowJSON`, `OrderRowJSON`, `MemberTotalJSON`,
`ExportMembersJSON` and `State` artifacts. Projection-only packages such as `reports`
become ordinary rows declarations with empty ownership manifests after cleanup.
Within goway, retain `integrations` DTO, `models` enums and `reporting` enum;
`oldmodels` enums and `oldreporting` contracts belong to the frozen-oracle decision,
not blanket deletion. The plugin fixtures have no model-output cleanup to perform.

Retained descriptor identities, manifest/OpenAPI/TypeScript and transport bytes must
remain identical. Ownership JSON is expected to change; wire artifacts are not.
Keep the source-doc cleanup path until old managed blocks and outstanding journals
are handled; removing `--field-docs` from Makefile alone does not perform cleanup.

## 4. Generator test retirement ledger

Delete the model-only files below only after the named replacement assertions are
present and accepted. Paths in the left column are under `internal/generate/`;
`goway/` below means `tests/fixtures/goway/`. These are coverage destinations, not
a claim that every old diagnostic is already reproduced. Compiler cases must reject
the intended owner/type/phase, never merely an undefined generated identifier.

| Delete | Replacement coverage to preserve / complete |
| --- | --- |
| `aggregates_test.go` | `goway/relation_aggregates_golden_test.go`, `database/orm/relation_aggregate_test.go`; aggregate owner/input/result negatives. |
| `binary_model_test.go` | `consumer/binarymodels` ownership assertions, `database/orm/column_role_test.go`, `goway/sensitive_golden_test.go`; primary-type rejection. |
| `encrypted_model_test.go` | `database/orm/sensitive_declaration_test.go`, `goway/sensitive_golden_test.go:TestSensitiveOldAndNewAgree`; invalid encrypted roles. |
| `password_model_test.go` | `database/orm/password_mutator_test.go`, `goway/sensitive_golden_test.go:TestPasswordOldAndNewAgree`; sensitive identity rejection. |
| `extensions_test.go` | `goway/extension_handles_test.go`, migrated `consumer/articles` and `profiles`; owner/policy/contract/unbound negatives. Retain DTO metadata generation tests. |
| `interval_test.go` | `consumer/intervalqueries/orm_test.go`, ORM expression/codec interval tests and interval input negatives. |
| `join_test.go` | `goway/joins_golden_test.go`, `on_conditions_golden_test.go`, scoped/nullable join negatives. |
| `model_audit_test.go` | `database/orm/audit_test.go`, `goway/audit_golden_test.go`, `consumer/auditqueries`. |
| `model_changes_test.go` | `goway/lifecycle_golden_test.go:TestLifecycleOldAndNewAgree`, native change/ignored-field negatives. |
| `model_each_test.go` | `database/orm/write_each_test.go`, `write_each_soft_test.go`; preparation/lifecycle/unknown-outcome/ownership cases. |
| `model_global_scopes_test.go` | `database/orm/global_scope_test.go`, `scope_source_failure_test.go`, `goway/retrieval_scope_golden_test.go`. |
| `model_hooks_test.go` | `database/orm/observer_capability_test.go`, `goway/lifecycle_golden_test.go`, native hook/observer compile-fail cases. |
| `model_insert_select_test.go` | `goway/source_write_golden_test.go:TestSourceWriteOldAndNewAgree`, `database/orm/rows/source_write_test.go`; input/storage/primary mapping negatives. |
| `model_lookup_test.go` | `database/orm/lookup_review_test.go`, `goway/terminal_writes_golden_test.go`, migrated native lookup consumers. |
| `model_reference_test.go` | `database/orm/relation_test.go`, migrated `consumer/nestedbindings`, `http/modelbinding` native resolver tests; foreign-key/owner negatives. |
| `model_retrieval_hooks_test.go` | `goway/retrieval_scope_golden_test.go`, `read_terminal_golden_test.go`; native retrieval registration, cancellation and batch tests. |
| `model_source_write_test.go` | `database/orm/source_write_test.go`, `source_write_bridge_test.go`, `goway/source_write_golden_test.go`. |
| `mutation_input_test.go` | `database/orm/column_role_test.go`, `goway/mutators_golden_test.go`, native UUID/JSON/distinct-input/nullable-role negatives. |
| `mutators_test.go` | `database/orm/mutator_test.go`, `goway/mutators_golden_test.go`, migrated `consumer/mutatorqueries`. |
| `projection_test.go` | `database/orm/rows/projection_test.go`, `record_test.go`, `goway/records_golden_test.go:TestStage6c1RecordGolden`; projection owner/nullability/codec cases. |
| `projection_dto_test.go` | `goway/report_projection_golden_test.go:TestReportProjectionGolden`, `consumer/reporting` DTO/rows contracts and `clientcontracts` byte checks; keep ordinary DTO collision coverage. |
| `relation_writes_test.go` | `database/orm/relation_write_test.go`, `relation_write_review_test.go`, `goway/relation_writes_test.go`; default input/owner/UUID negatives. |
| `relations_test.go` | `goway/relations_golden_test.go:TestDirectRelationSQLMatchesOldAPI`, `relations_joined_test.go`, native relation descriptor negatives. |
| `soft_deletes_test.go` | `database/orm/column_role_test.go`, `write_each_soft_test.go`, migrated `consumer/softqueries`; scope/opt-out/restore cases. |
| `timestamps_test.go` | `database/orm/column_role_test.go`, `goway/lifecycle_golden_test.go`, migrated `consumer/timequeries`; managed roles and opt-out cases. |
| `closure_guard_test.go` | Replace its generated-model closure traversal with the native public-surface/capability checks in `database/orm/method_surface_test.go`, `scope_types_test.go`, `observer_capability_test.go`, `goway/extension_handles_test.go`. Preserve an executable negative proving the new closure guard catches a deliberately leaked legacy family; add it before deleting this file if existing checks do not cover that reachability. Keep `transaction_phase_test.go`'s convertibility sweep. |

Mixed tests require surgery, not deletion:

- `generate_test.go`: replace model fixtures in fresh/stale/ownership/rollback and
  alias reachability tests with DTO/enum examples. Move model ID ownership and
  generated SQL integer-range assertions to native declaration/codec coverage.
- `generate_diagnostics_test.go`, `directive_forms_test.go`: retain enum diagnostics,
  cached descriptors, collision positions, header stability, retired-directive
  diagnostics and all retained directive forms. Drop only old model suffix cases.
- `graph_test.go:TestGraphCyclesAndDuplicateTables`: retain graph-cycle coverage;
  duplicate SQL tables move to the table-registration/startup owner. Keep graph
  selection/import/overlay/fresh dependency tests, using retained declarations.
- `json_test.go`: preserve path naming stability, nested/flat distinction, malformed
  tags, local-name shadowing, recursion and shape tests under the retained contract
  entry point. Native SQL coverage is `goway/json_path_golden_test.go` and
  `database/orm/expr/json_declaration_test.go`; it does not replace generation tests.
- `field_documentation_test.go`: retire model accessor/notices expectations;
  native accessor validation belongs in ORM declaration/mutator tests. Preserve
  managed-source token/comment and cleanup publication tests.
- `source_documentation_test.go`: replace model/projection fixtures in original-help
  propagation tests with retained DTO/config/path contracts. Keep source links,
  alias/generic/promotion handling, stale-input detection and independent graph state.
- `dto_test.go`, `dto_map_key_test.go`, `multipart_test.go`, `union_test.go`: retain
  transport model-rejection tests, rewritten to native declarations after the
  section 1 decision. Keep IDs/enums/custom codecs and all wire behavior.
- Keep `field_documentation_recovery_test.go`, `recover_test.go`, `distribution_test.go`,
  `artifacts_test.go` and general publication tests. Use retained DTO/enum inputs for
  normal generation; retain historical model-shaped journals where needed to prove
  recovery across upgrade. Keep all current ORM scaffold tests and goldens.

Add named retirement regressions before deletion: `TestRetiredDirectivesDoNotPublish`,
`TestModelOutputRetirementKeepsMixedContracts`,
`TestRetirementVisitsDirectiveFreeOwnedPackages`, and
`TestRetirementPreservesRetainedContractBytes`. These are proposed tests, not results.
Freeze old/new SQL assertions before changing an oracle; the ledger does not authorize
its removal or classify newly rewritten tests as already passing.

## 5. Current consumer blockers and owners

Static inspection included generated package-level symbol references in handwritten
production, test and negative sources, not just imports. It found references in
183 consumer negative directories; this is a conservative lexical workload count,
not 183 confirmed unported rules (comments and same-name native declarations can
match, and generated method-only uses need an additional audit). Reconcile with
each lane's accepted patch and catalogue at integration rather than relabelling all
these matches as defects. No source in another worktree was treated as integrated.

| Owner | Present blockers / concrete anchors |
| --- | --- |
| A | Runtime migrations are accepted. Coexistence factories remain in `officequeries/models.go`, `pivothooks/{models,hooks}.go`, bootstrap/productionprofile declarations; explicit legacy SQL oracles remain in `officequeries/{tables,related_value}_test.go`, `pivothooks/tables_test.go`, `bootstrap/tables_test.go`, `correlations/sql_test.go`, `httpmodels/sql_parity_test.go`, `tooling/database_test.go`. `nested_field_owner` and `nested_relation_owner` await D's nestedbindings in batch 7e. Keep their native replacements and remove old sides only under the oracle policy. |
| C (A1 + SQL) | Shared coexistence code: `models/models.go:CurrentEmailDraft`, `models/{relations,aggregates,membership}.go`, `catalog/product.go:CreatorDraft`/`defineProductRelations`, model/catalog ORM parity tests, `reports/rows_test.go`. Legacy assertions still occur in `advancedqueries`, `comparisonqueries`, `ctes`, `cursorqueries`, `distinctqueries`, `expressionqueries`, `filterqueries`, `framequeries`, `intervalqueries`, `jsonqueries`, `keyqueries`, `lateralqueries`, `pagequeries`, `projectionqueries`, `recordqueries`, `recursivequeries`, `reporting`, `scalarqueries`, `setqueries`, `temporalqueries`, `transactionqueries`, `windowqueries`. Some are deliberate old/new tests; others are retained PostgreSQL tests. C must classify/port each, including SQL negatives. |
| B (writes/lifecycle + reassigned services) | Legacy factories/bridges still present in `auditqueries`, `binarymodels`, `hookqueries`, `inputqueries`, `linkqueries`, `mutatorqueries`, `observerqueries`, `retrievalqueries`, `softqueries`, `tenantqueries`, `timequeries`, plus model/projection directives in `vault`. `linkqueries/legacy_creation_bridge_postgres_test.go`, `retrievalqueries/legacy_iteration_postgres_test.go` and `upsertqueries/*_postgres_test.go` retain old assertions. `authenticating/{accounts,composed_routes}.go` and `recovering/{credentials,recovery,hooks}.go` still call generated APIs in this checkout. B owns their negatives, D2 adapters and clientcontracts Go migration per decisions 10–12; accepted sibling changes must be integrated before cleanup. |
| D (accepted consumers awaiting integration / D4) | Current generated use in `articles` (including `tables.go`, `http.go` and PostgreSQL tests), `httppagination/pagination.go`, `idempotenthttp/application.go`, `isolatedhttp/application.go`, `passwords/{account,login}.go`, `multifactor/{account,security}.go`, `teamworkflow/{models,service,routes}.go`, `nestedbindings/{models,bindings}.go`; profiles parity tests. Several have already been accepted in D's worktree per the lead, but are not present here. D owns root harness, editor and footprint cleanup, plus its remaining negatives. |
| D root harness | `consumer/{model_changes,model_query_postgres,model_write_postgres,model_relations_postgres,model_through_postgres,model_aggregates_postgres,model_pagination_postgres,observer_ownership,codecs,generated,package_graph,orm_benchmark}_test.go`; `testdata/footprint/model_draft/probe.go`. Preserve assertions and freeze legacy measurements before replacing the draft probe. |
| D editor/tools; lead generation | `internal/agent/gopls_acceptance_test.go` still expects generated drafts/fields/relations and paths into generated files. `tools/measure-release.py` uses `models.UserDraft{}.SetID`; `tools/goway_synthetic.py` and its tests deliberately generate old model/projection programs. Supply native editor/release anchors; explicitly freeze/retire old synthetic generation rather than silently dropping comparison coverage. Catalogue scanner test strings remain useful and are not live generated consumers. |
| Lead + original slice owners | `goway/oldmodels` and children `extensions`, `hooks`, `joined`, `mutators`, `reads`, `sensitive`; `goway/oldreporting`; old/new golden suites and legacy footprint sources. Decision 5 preserves these until 8d. Resolve section 1 before 8c publication. |

I-only HTTP, DTO, config and plugin packages are retained, not blockers merely for
importing model IDs. Old `database/query` aliases/imports without generated-symbol
use belong to 8d, not automatically to 8c. Conversely, leftover declaration
factories are compile dependencies even when ordinary execution has migrated.
Include test code strings, build-tagged PostgreSQL files and compiler fixtures in
the final caller audit; import-only searches miss same-package calls and methods.

## 6. Execution order and risks

1. **Close the caller barrier.** Integrate Stage 9/batch 7e, finish the two A nested
   negatives, settle oracle and retained JSON/DTO-boundary decisions, and let each
   owner remove obsolete handwritten factories. D updates editor/harness anchors.
   Freeze legacy SQL/footprint evidence and retained contract bytes.
2. **Separate retained helpers and coverage.** Move `fieldCodec` once; preserve
   path/DTO/model-boundary behavior. Convert mixed generator fixtures; add retirement
   and cleanup tests. Finish any uncovered native diagnostics in the ledger. No
   Query/Table methods, new capability escape, or zero-value authority.
3. **Remove emitters and publish once.** Apply section 2's deletions/surgery, clean
   managed model notes safely, and let the lead run Makefile generation across all
   retained and cleanup roots. Review manifest removals separately from retained
   contract bytes. Keep recovery able to handle pre-upgrade journals.
4. **Lead checkpoint and handoff to 8d.** Verify generator/unit and fresh/stale graph,
   collision, publication/recovery/distribution/scaffold coverage; native compiler
   catalogue, surface and convertibility guards; consumer build/vet/race, compiled
   PostgreSQL sources, editor probes, generated-output and contract-byte gates.
   Lead owns live PostgreSQL/TypeScript/full integration checks. Existing footprint
   ceilings never rise (`q_with` is full); measure any new probe at ceil(x1.15).
   Use the then-current mandated cache (currently r10) and CPU limits. This plan
   schedules these checks; it does not execute or claim them.

Primary risks are dangling coexistence code, unreachable retained JSON generation,
weakened model/DTO rejection, deleting enum/codec helpers with model files, changed
wire identities, skipping directive-free manifests, and damaged source/journal
recovery. Keep the deletion patch mechanically narrow. Stop on contract byte drift
or a missing replacement assertion; do not make generators, budgets or catalogue
checks accept the loss. Shared changelog/blueprint status updates remain lead-owned.
