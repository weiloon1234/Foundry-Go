# Guide/proof pilot and pinned documentation integration

Milestones 1 and 2 integration handoff, 2026-11-07, on `lane/s6-guides`.
The lead replayed milestone 2 (`6a0162de`) onto milestone 1 (`228d5403`).
The initial integration preserved fixture sources, generated fences, CLI selection,
packaging and Makefile ownership. Fix round 1 below tightens CLI module identity
and documentation checks; fixture sources and rendered fences remained unchanged
through that round. Fix round 2 below migrates the remaining query excerpts.

The lead reports these prior results:

- Milestone 1: `make generate`, `make generate-check`, pre-replay `make docs-check`,
  and tests for `internal/docsource`, `internal/cmd/renderdocs`,
  `internal/cmd/checkdocs` and `internal/testinputs` passed.
- Milestone 2: tests for `internal/docrefs`, `internal/retirement` and
  `cmd/foundry` passed. Its standalone docs check found former-API teaching in
  the v0.2 migration and active validation guides; this integration addresses it.
- Fixture compilation and TypeScript proof are still pending. The packaged
  offline test is retained; no result for it was supplied in this handoff.

No tests, builds, renderer runs or verification commands ran during integration.
Touched Go files were formatted. Earlier results do not verify this revision;
the lead owns the commands below. This is implementation evidence, not milestone
acceptance or a release claim.

## Fix round 2 after 29340bbf

The lead reports that the round 1 targeted tests (safeerror, docsource, docrefs,
checkdocs, renderdocs, testinputs, retirement, frameworkinfo, doctor, cmd/foundry
and generate), `make generate-check` and `make docs-check` passed. `make verify`
then passed `make test` and stopped at the legacy `TestGuideExcerpts` fence census
in fixture-check. These results predate this round; fixture completion and the
TypeScript proof remain pending.

This round converts all 36 `go-source` directives: 33 become named
`compiled-source` regions and three duplicates become links. All 28
`compiled-examples` markers are removed. The shared docsource inventory retains
whole-guide coverage for all 28 guides, alongside its original ten complete guides.
Active legacy markers now fail parsing, preventing the old convention from returning.
No new fixture functions or executable statements were needed.

The deleted `tests/fixtures/goway/guideexamples/guides_test.go` has no remaining
fence-matching responsibility. `internal/docsource` owns equality and coverage;
fixture-check owns actual compiler-input membership. Its required
`(orm-migration.md)` link and generated model-name patterns now live in
`internal/cmd/checkdocs/migration_guides.go`, using the same guide inventory.
The old `query.X` regex is already covered by `internal/retirement/docs.go` and
is not duplicated. Regression sources cover missing links, each remaining generated
form, current typed query teaching and the repository-level retirement check.

The owner links replacing duplicate fences are:

| Removed copy | Owning example |
| --- | --- |
| typed-field-computations selected aggregate | [Conditional expressions](../guides/conditional-expressions.md#selected-aggregates-and-windows) |
| temporal-calculations calendar/elapsed comparison | [Interval calculations](../guides/interval-queries.md#calculations) |
| datatable token-ticket adapter | [Token tickets](../guides/tokens.md#websocket-handshake-tickets) |

The DTO region separates its begin marker from `//foundry:dto` with a blank line.
Named-region rendering now drops leading/trailing blank lines before removing common
indentation. Interior blank lines and whole-file boundary blank lines remain.
Tests cover empty/padded regions, preserved interior whitespace, whole-file behavior,
read-only comparison, and rendering with the same padding rule. No empty `//` line
was added to the guide.

Retained fence bodies have not been hand-edited. The datatable standard-routes fence
uses spaces where its source uses tabs; the lead's `make generate` must apply the
common-indent rendering normalization. All other retained legacy fence bodies matched
the source after common-indent removal when the migration was prepared. No excerpt
needed splitting or a teaching change.

### Snippet-convention audit

Repository searches covered `-source:` directives, snippet-test names, Markdown
reads in tests, and `strings.Contains` comparisons against guide/README text.

- `internal/docsource/repository_test.go` owns compiled-source equality and TS input
  membership; renderdocs tests cover rendering and read-only comparison. These use
  the shared mechanism, not another snippet convention.
- `tests/fixtures/goway/migration/guide_test.go` deliberately remains a **second
  mechanism**: `orm-example` labels and `migration`/`guide` source regions. Its
  `TestGuideSnippets` checks ORM migration examples; `TestLifecycleGuideSnippets`
  checks the lifecycle guide census. Removed `*-before` APIs cannot compile and
  remain labeled migration teaching under the retirement exception. The existing
  checker delegates compiled-source equality to docsource. Its before/after markers,
  fences and census are unchanged in this round.
- `tools/release/documentation_test.go` checks packaged guide bytes and anchors,
  not source excerpts. `tools/release/release_test.go` mentions the old checker
  filenames only as synthetic packaging-exclusion fixtures.
- `tools/goway_catalogue_test.py` reads a blueprint README for catalogue metadata.
  Generator documentation/scaffold tests and agent completion tests compare generated
  Go or IDE documentation, not guide/README snippets. Devserver, i18n and deployment
  README tests concern file handling, not excerpt equality.

No third guide/README snippet convention was found. The migration labels are
intentional; they are not candidates for compiling removed APIs.

### Round 2 handoff commands

No tests, builds, renderer runs or verification commands were run for this round.
Touched Go files were formatted. From this worktree, with the existing native
TypeScript environment configured:

```sh
export GOWORK=off
export GOTOOLCHAIN="$(awk '$1 == "toolchain" { print $2 }' go.mod)"
make generate
make generate-check
make docs-check
go test -count=1 ./internal/docsource ./internal/cmd/renderdocs ./internal/cmd/checkdocs ./internal/retirement
(cd tests/fixtures/goway && go test -count=1 ./migration -run '^(TestGuideSnippets|TestLifecycleGuideSnippets)$')
make fixture-check
make typescript-check
```

Alternatively, after generation and the focused tests, `make verify` supplies the
combined gate, including fixture and required TypeScript proof; do not duplicate
those expensive runs in the same checkpoint. There are no open design questions.

### Round 2 files

- [docs/evidence/goway-stage10-lane-c.md](goway-stage10-lane-c.md): replace the deleted checker's historical link with its current owner.
- [CHANGELOG.md](../../CHANGELOG.md)
- [docs/evidence/guide-proof-pilot-20261107.md](../../docs/evidence/guide-proof-pilot-20261107.md)
- [docs/guides/computed-query-keys.md](../../docs/guides/computed-query-keys.md)
- [docs/guides/conditional-expressions.md](../../docs/guides/conditional-expressions.md)
- [docs/guides/datatable.md](../../docs/guides/datatable.md)
- [docs/guides/developer-tooling-and-testing.md](../../docs/guides/developer-tooling-and-testing.md)
- [docs/guides/geographic-distance.md](../../docs/guides/geographic-distance.md)
- [docs/guides/interval-queries.md](../../docs/guides/interval-queries.md)
- [docs/guides/json-models.md](../../docs/guides/json-models.md)
- [docs/guides/lateral-joins.md](../../docs/guides/lateral-joins.md)
- [docs/guides/model-aggregate-filters.md](../../docs/guides/model-aggregate-filters.md)
- [docs/guides/model-aggregates.md](../../docs/guides/model-aggregates.md)
- [docs/guides/model-chunks.md](../../docs/guides/model-chunks.md)
- [docs/guides/model-correlations.md](../../docs/guides/model-correlations.md)
- [docs/guides/model-ctes.md](../../docs/guides/model-ctes.md)
- [docs/guides/model-distinct.md](../../docs/guides/model-distinct.md)
- [docs/guides/model-joins.md](../../docs/guides/model-joins.md)
- [docs/guides/model-projections.md](../../docs/guides/model-projections.md)
- [docs/guides/model-queries.md](../../docs/guides/model-queries.md)
- [docs/guides/model-recursive-ctes.md](../../docs/guides/model-recursive-ctes.md)
- [docs/guides/model-set-operations.md](../../docs/guides/model-set-operations.md)
- [docs/guides/model-subqueries.md](../../docs/guides/model-subqueries.md)
- [docs/guides/model-windows.md](../../docs/guides/model-windows.md)
- [docs/guides/scalar-calculations.md](../../docs/guides/scalar-calculations.md)
- [docs/guides/temporal-calculations.md](../../docs/guides/temporal-calculations.md)
- [docs/guides/transaction-correlations.md](../../docs/guides/transaction-correlations.md)
- [docs/guides/transaction-query-composition.md](../../docs/guides/transaction-query-composition.md)
- [docs/guides/trigram-search.md](../../docs/guides/trigram-search.md)
- [docs/guides/typed-field-computations.md](../../docs/guides/typed-field-computations.md)
- [docs/guides/value-comparisons-and-joins.md](../../docs/guides/value-comparisons-and-joins.md)
- [docs/guides/window-ranges-and-names.md](../../docs/guides/window-ranges-and-names.md)
- [internal/cmd/checkdocs/main.go](../../internal/cmd/checkdocs/main.go)
- [internal/cmd/checkdocs/migration_guides.go](../../internal/cmd/checkdocs/migration_guides.go)
- [internal/cmd/checkdocs/migration_guides_test.go](../../internal/cmd/checkdocs/migration_guides_test.go)
- [internal/cmd/renderdocs/main_test.go](../../internal/cmd/renderdocs/main_test.go)
- [internal/docsource/document.go](../../internal/docsource/document.go)
- [internal/docsource/source.go](../../internal/docsource/source.go)
- [internal/docsource/source_test.go](../../internal/docsource/source_test.go)
- [tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go](../../tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go)
- [tests/fixtures/consumer/clientcontracts/table_bindings.go](../../tests/fixtures/consumer/clientcontracts/table_bindings.go)
- [tests/fixtures/consumer/comparisonqueries/joins_postgres_test.go](../../tests/fixtures/consumer/comparisonqueries/joins_postgres_test.go)
- [tests/fixtures/consumer/correlations/correlations_postgres_test.go](../../tests/fixtures/consumer/correlations/correlations_postgres_test.go)
- [tests/fixtures/consumer/ctes/ctes_postgres_test.go](../../tests/fixtures/consumer/ctes/ctes_postgres_test.go)
- [tests/fixtures/consumer/distinctqueries/distinct_postgres_test.go](../../tests/fixtures/consumer/distinctqueries/distinct_postgres_test.go)
- [tests/fixtures/consumer/expressionqueries/expressions_test.go](../../tests/fixtures/consumer/expressionqueries/expressions_test.go)
- [tests/fixtures/consumer/framequeries/named_postgres_test.go](../../tests/fixtures/consumer/framequeries/named_postgres_test.go)
- [tests/fixtures/consumer/framequeries/ranges_postgres_test.go](../../tests/fixtures/consumer/framequeries/ranges_postgres_test.go)
- [tests/fixtures/consumer/intervalqueries/calculation_postgres_test.go](../../tests/fixtures/consumer/intervalqueries/calculation_postgres_test.go)
- [tests/fixtures/consumer/intervalqueries/orm.go](../../tests/fixtures/consumer/intervalqueries/orm.go)
- [tests/fixtures/consumer/intervalqueries/orm_test.go](../../tests/fixtures/consumer/intervalqueries/orm_test.go)
- [tests/fixtures/consumer/joinqueries/outer/outer_postgres_test.go](../../tests/fixtures/consumer/joinqueries/outer/outer_postgres_test.go)
- [tests/fixtures/consumer/jsonqueries/path_sql_test.go](../../tests/fixtures/consumer/jsonqueries/path_sql_test.go)
- [tests/fixtures/consumer/jsonqueries/tables.go](../../tests/fixtures/consumer/jsonqueries/tables.go)
- [tests/fixtures/consumer/keyqueries/keys_test.go](../../tests/fixtures/consumer/keyqueries/keys_test.go)
- [tests/fixtures/consumer/lateralqueries/records_postgres_test.go](../../tests/fixtures/consumer/lateralqueries/records_postgres_test.go)
- [tests/fixtures/consumer/projectionqueries/aggregate_contracts_test.go](../../tests/fixtures/consumer/projectionqueries/aggregate_contracts_test.go)
- [tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go](../../tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go)
- [tests/fixtures/consumer/reporting/member_import.go](../../tests/fixtures/consumer/reporting/member_import.go)
- [tests/fixtures/consumer/reporting/member_import_test.go](../../tests/fixtures/consumer/reporting/member_import_test.go)
- [tests/fixtures/consumer/reporting/models.go](../../tests/fixtures/consumer/reporting/models.go)
- [tests/fixtures/consumer/reporting/reporting_postgres_test.go](../../tests/fixtures/consumer/reporting/reporting_postgres_test.go)
- [tests/fixtures/consumer/reporting/table_routes.go](../../tests/fixtures/consumer/reporting/table_routes.go)
- [tests/fixtures/consumer/scalarqueries/scalars_postgres_test.go](../../tests/fixtures/consumer/scalarqueries/scalars_postgres_test.go)
- [tests/fixtures/consumer/setqueries/editor_examples_test.go](../../tests/fixtures/consumer/setqueries/editor_examples_test.go)
- [tests/fixtures/consumer/temporalqueries/orm_test.go](../../tests/fixtures/consumer/temporalqueries/orm_test.go)
- [tests/fixtures/consumer/transactionqueries/correlations_postgres_test.go](../../tests/fixtures/consumer/transactionqueries/correlations_postgres_test.go)
- [tests/fixtures/consumer/transactionqueries/models.go](../../tests/fixtures/consumer/transactionqueries/models.go)
- [tests/fixtures/consumer/windowqueries/windows_postgres_test.go](../../tests/fixtures/consumer/windowqueries/windows_postgres_test.go)
- Deleted `tests/fixtures/goway/guideexamples/guides_test.go`.

## Fix round 1 after a1f7b751

The lead's `make verify` stopped in `make test` at
`TestRuntimeErrorConstructionUsesDiagnosticOwners`. Fixture-check, generate-check,
docs-check and the TypeScript proof had not run on that revision. No tests,
builds, generators or verification commands ran during this fix round. Source
review and Go formatting are the only local implementation checks.

| Item | Change and regression sources |
| --- | --- |
| Diagnostic ownership | `docsource`, `docrefs` and the docs CLI now use safeerror construction owners and joins. One typed `docrefs.Problem` constructor captures its immutable message; its disposition is registered in the existing source contract. Tests retain typed locations, aggregate messages and singleton identity. No presentation exemption was added. |
| S6-R01 | Canonicalize the app root before module discovery. Reject active skill-directory symlinks before filename filtering. CLI tests cover clean/stale symlinked roots, each active parent and the excluded mirror. |
| S6-R02 | Require each entry/recipe anchor to bind to one ATX heading. Tests reject intervening comments/prose, consecutive anchors, setext headings and EOF, including linked recipes with unbound Go fences. |
| S6-R03 | Go scanner tokens and TypeScript comment scanning establish actual line-comment positions. Tests reject markers in raw/template strings and block comments despite compiler-input membership; valid standalone/trailing comments retain coverage. |
| S6-R04 | Normalize imported pagination owners and add all three authenticated aliases and their common constructor owner to the retirement ledger. Tests cover removed scopes, permissions and authorization, plus retained package functions and external owners. |
| S6-R05 | Register transaction-scalar and transaction-correlated row constructors/types, including projection construction. Tests preserve locking through direct constructors, bindings and qualified types while still rejecting ORM query method locking. |
| S6-R06 | Carry effective replacement path/version through frameworkinfo, doctor and generator selection. CLI and frameworkinfo tests reject equal-version cross-module replacements and accept matching identities; generator expectations cover both outcomes. |
| S6-R07 | Framework docs-check parses active instruction/guide/skill declarations with docrefs and resolves against its guide tree. Tests cover missing pages/fragments, malformed trailing text and ignored fenced declarations. |
| S6-R08 | Resolve used full, collapsed and shortcut Markdown references through the existing local-path/fragment checks. Tests cover missing files/anchors, valid targets, unused definitions and fenced/code examples. |

Focused reruns for the lead, from this worktree with the native toolchain selected:

```sh
go test -count=1 ./internal/safeerror -run '^TestRuntimeErrorConstructionUsesDiagnosticOwners$'
go test -count=1 ./internal/docsource ./internal/docrefs ./internal/cmd/checkdocs ./internal/retirement ./internal/frameworkinfo ./internal/doctor ./cmd/foundry
go test -count=1 ./internal/generate -run '^TestFrameworkVersionMismatchFailsBeforeWriting$'
```

Then resume the combined gate below or `make verify`. Its later prerequisites
remain pending; prior results do not verify the fix round. No review finding was
disputed, and no design question is open.

## Acceptance mapping

| Brief item | Implementation |
| --- | --- |
| Region parser and exact rendering | [internal/docsource](../../internal/docsource) parses standalone or trailing boundary comments, validates unique region names, rejects overlap, removes markers, named-region boundary blank lines and common indentation, selects a safe delimiter and derives the language. Fragmentless directives select the whole file. |
| Generation ownership | [renderdocs](../../internal/cmd/renderdocs) renders or compares; [Makefile](../../Makefile) calls it after declaration generation in `generate` and read-only `generate-check`. |
| Documentation checks | [checkdocs](../../internal/cmd/checkdocs) delegates equality, unique owning inclusion, approved source roots, complete-guide coverage, recipe fence binding and navigation-only entries to docsource. Explicit anchor indexing and `agent-entry`/`recipe-*` link checks now delegate to docrefs; Agent entry line locations come from the same docsource section parser. |
| Existing error examples | Both Go declarations and TypeScript narrowing now name explicit regions. The TypeScript create-member behavior is unchanged. |
| Pilot guides | Existing owners have task navigation and explicit `agent-entry` and `recipe-*` anchors. The table below records the teaching reconciliation. |
| Compilation proof | `fixture-check` collects actual Go files selected by `go list -json ./...` in its fixture loop. Docsource compares the directive inventory against those inputs, including test files and excluding ignored files. TS examples share the actual strict compiler lists with the current-client exporters. |
| Required TypeScript | `verify` requires the selected tool paths and exports `FOUNDRY_TEST_TYPESCRIPT_REQUIRED=1` to its fixture run. The existing CI environment remains unchanged. No second TypeScript acceptance run is added to verify. |
| Tests and authoring | Renderer, read-only comparison, malformed markers, ownership, paths, section coverage and input-list tests are written. Four receiver-family compiler negatives and a conditional error-export negative were added. The [authoring workflow](../guides/developer-tooling-and-testing.md#author-compiled-guide-examples) and [Unreleased changelog](../../CHANGELOG.md) describe the change. |
| Legacy migration | Every existing compiled-source directive names an explicit region. Both guideadapters and guideexamples substring tests are removed; their complete-guide policies and repository equality test now live in docsource. Query migration links and generated-API rejection remain in checkdocs and retirement. The separate historical ORM migration census still checks its legacy examples, handing source-bound fence equality to docsource. |
| Pinned references and offline checking | [docrefs](../../internal/docrefs) owns reference grammar and explicit anchor indexing. [The CLI](../../cmd/foundry/docs.go) resolves the selected release offline, scans active module instructions and applies its retirement rules. [The packaged consumer test](../../tools/release/documentation_test.go) retains source-free, offline acceptance coverage. |
| Narrow migration exception | [Retirement documentation checks](../../internal/retirement/docs.go) extend the existing `orm-example: name-before` label to one adjacent fence, table or paragraph in only the ORM and v0.2 migration guides. Active guides and app instructions cannot claim it. Validation's former-method mapping has one owner in the v0.2 guide. |
| Integration regression sources | Tests cover label boundaries and denied active/app exceptions; missing, duplicate, malformed and fenced anchors; same-page/cross-guide fragments; Agent entry targets and section boundaries; read-only checking; unchanged handling of other fragments. These tests have not run on the integrated revision. |

Standalone end comments after top-level declarations acquire blank lines under
gofmt. Trailing end markers avoid changing the excerpt's bytes. A trailing begin
marker on the `Settings` declaration also leaves its existing Go configuration
directive outside that excerpt. These comments do not change compiled behavior.

## Recipe ownership from the three Starter skills

App-specific filenames, registries, actor choices, export profiles, verification
cadence and the app's pending form-controller adoption remain app policy. No app
repository or skill file was changed. Existing owner prose retains lifecycle,
authorization, cleanup and uncertainty rules; compilation does not replace their
behavior tests.

| Skill teaching | Owner | Compiled region or context |
| --- | --- | --- |
| Model declarations, mutation input, managed timestamps and soft deletion | [Declare tables](../guides/model-declarations.md#recipe-declare-table), [soft deletion](../guides/model-declarations.md#recipe-soft-delete) | `migration/examples_test.go`: `roles-after`, `force-after` |
| Complete table registration and stored typed identity | [Register tables](../guides/model-declarations.md#recipe-register-tables) | `application-after`, `identity-after` in the same migration fixture |
| Typed reads and keyed lookups | [Read models](../guides/model-queries.md#recipe-read-models) | `reads-after` in the migration fixture |
| Predicates, expression adapters and nullable computed text | [Filter expressions](../guides/model-queries.md#recipe-filter-expressions) | `scalarqueries/text_row_test.go`: `computed-text` |
| Input assignments, NULL and write scope | [Write models](../guides/model-writes.md#recipe-write-models) | `migration/lifecycle_test.go`: `insert-member`; `migration/examples_test.go`: `writes-after` |
| Protect a unique-column change in the caller's transaction | [Lock and write](../guides/model-writes.md#recipe-lock-write) | `unique-column-lock` in the migration fixture; advanced locking remains reference reading |
| Relation loading and existing parents | [Load relations](../guides/model-relations.md#recipe-load-relations) | `relations-after` in the migration fixture |
| Row schemas, mapping, stable ordering and projection pages | [Declare rows](../guides/model-projections.md#recipe-declare-rows), [select rows](../guides/model-projections.md#recipe-select-rows) | `reports/rows.go`: `user-summary`; `reports/guide.go`: `paginate-projection` |
| Trigram candidates and scores in the same transaction | [Trigram search](../guides/trigram-search.md#recipe-trigram-search) | `trigramqueries/search.go`: `find-duplicates`, `similar-reviews` |
| Explicit accessors and input mutations | [Access fields](../guides/model-accessors.md#recipe-access-field), [mutate fields](../guides/model-mutators.md#recipe-mutate-field) | `accessors-after`, `inputs-after`; existing declaration/mutator prose owns omission, normalization and purity |
| Write hooks and typed audit policy | [Hooks](../guides/model-hooks.md#recipe-write-hooks), [audit](../guides/audit.md#recipe-register-audit) | `hooks-after`, `write-options`, `audit-registration`; audit owner retains Ignore/Exclude behavior |
| Endpoint scopes, descriptors, preparation, validation and service boundary | [Compose endpoints](../guides/http-endpoints.md#recipe-compose-endpoint) | `httpendpoints/guide.go`: `compose-endpoint`; service receives decoded values |
| DTO omission, null, zero and validation | [Presence](../guides/http-presence.md#recipe-presence) | `validationrules/presence.go`: `presence-input-rules`; existing HTTP tests retain runtime coverage |
| Resource lookup, parent scope and authenticated binding | [Bind resources](../guides/http-model-binding.md#recipe-bind-resource), [authenticated binding](../guides/http-model-binding.md#recipe-authenticated-binding) | Existing `httpmodels/bindings.go` regions; `authenticating/composed_routes.go`: `authenticated-resource`; nested lookup remains at scoped-model-binding |
| Public lists, transport pages and explicit resource mapping | [Numbered pages](../guides/http-pagination.md#recipe-numbered-page), [authenticated pages](../guides/http-pagination.md#recipe-authenticated-pages) | Existing pagination and authenticated fixtures, now explicit regions |
| Detailed public errors, exact declarations and service error mapping | [Error details](../guides/http-application-errors.md#recipe-error-details) | `clientcontracts/member_create.go`: declaration, endpoint and handler regions |
| Generated operation/code error narrowing | [Client narrowing](../guides/http-application-errors.md#recipe-client-narrowing) | `clientcontracts/testdata/error-details.ts` |
| Portal entries, operation restriction and shared identities | [Portal entries](../guides/client-contracts.md#recipe-portal-entries) | `testdata/surfaces.ts`: `portal-entries`; includes absent detailed-error guard export |
| Exact values, presence, locale-keyed inputs and field descriptors | [Wire values](../guides/client-contracts.md#recipe-wire-values) | `testdata/guide-recipes.ts`: `wire-values`, `locale-fields`; existing `types.ts` retains owner/value/omission negatives |
| Fetch and portal credential lifecycle | [Fetch](../guides/client-contracts.md#recipe-fetch-transport), [sessions](../guides/client-contracts.md#recipe-portal-session) | `testdata/fetch-transport.ts`: `fetch-client`; `guide-recipes.ts`: `portal-lifecycle` |
| Explicit table bindings and typed registries | [Table bindings](../guides/client-contracts.md#recipe-table-bindings) | `testdata/table-bindings/types.ts`: `table-registries`, `table-download`, `table-adapter`; existing Go manifest owns associations |
| Form input precision and disposal | [Form values](../guides/client-forms.md#recipe-form-values) | `testdata/guide-recipes.ts`: `form-values`; existing types/forms/adapter tests retain full controller coverage |

Schema types, enum annotations, constructor families and terminal signatures stay
in their existing reference explanations. Scaffolding, advanced query families,
transport-specific DTO details and app export workflows remain linked reference
reading rather than a second recipe catalog.

## Approved normalization and single owners

The lead approved and rendered the common-indent normalization in
`model-extension-slots.md:203` from `articles/http.go#article-input-rules`.
The former preserved mismatch is resolved; no symbol, call, rule or behavior
changed, and no checker exemption remains.

Two legacy excerpts were repeated teaching. `validation.md` owns the database-rule
example; `validation-expanded.md` links to it. `authentication.md` owns the ticket
adapter; `http-downloads.md` links to it. There is one source region per recipe.

## Lead commands

Use existing native tools and service configuration; install nothing as an
implicit side effect. The generation commands are listed for the combined gate;
this integration did not change fixture declarations or rendered fence contents.

```sh
export GOWORK=off
export GOTOOLCHAIN="$(awk '$1 == "toolchain" { print $2 }' go.mod)"
make generate
make generate-check
make docs-check
make fixture-check
make typescript-check

go test -count=1 ./internal/docsource ./internal/cmd/renderdocs ./internal/cmd/checkdocs ./internal/testinputs ./internal/docrefs ./internal/retirement ./cmd/foundry
(cd tools/release && go test -count=1 -run '^TestDocumentationPackagedOfflineConsumer$' .)

(cd tests/fixtures/consumer && go test -count=1 -run '^TestInvalidPublicContractsFailCompilation/guide_' .)
(cd tests/fixtures/goway && go test -count=1 ./migration -run '^(TestGuideSnippets|TestLifecycleGuideSnippets)$')
python3 tools/goway_catalogue.py
```

`FOUNDRY_TEST_NODE` and `FOUNDRY_TEST_TYPESCRIPT` must identify the existing native
Node executable and TypeScript package's `lib/tsc.js`. `make fixture-check` covers
all included Go packages; the compiler-negative command is available for a focused
failure rerun. `make typescript-check` exports fresh clients and runs strict typing
and existing runtime acceptance, including its table-bindings subtest. A checkpoint
running `make verify` already runs that proof through fixture-check; omit a separate
TypeScript run in that checkpoint to avoid duplicating the expensive client test.

The packaged test runs the app-facing `go tool foundry docs check --dir .`
against a selected candidate release with network access disabled. Running that
command against this development checkout is not a substitute: the CLI deliberately
rejects local replacements and unverifiable tool versions. Master milestone status
is unchanged pending the lead's combined acceptance. There are no open design
questions in this handoff.

## Fix round 1 files

- [CHANGELOG.md](../../CHANGELOG.md)
- [cmd/foundry/docs.go](../../cmd/foundry/docs.go)
- [cmd/foundry/docs_test.go](../../cmd/foundry/docs_test.go)
- [docs/compatibility.md](../../docs/compatibility.md)
- [docs/evidence/guide-proof-pilot-20261107.md](../../docs/evidence/guide-proof-pilot-20261107.md)
- [docs/guides/developer-tooling-and-testing.md](../../docs/guides/developer-tooling-and-testing.md)
- [internal/cmd/checkdocs/links.go](../../internal/cmd/checkdocs/links.go)
- [internal/cmd/checkdocs/main.go](../../internal/cmd/checkdocs/main.go)
- [internal/cmd/checkdocs/navigation_test.go](../../internal/cmd/checkdocs/navigation_test.go)
- [internal/docrefs/anchors.go](../../internal/docrefs/anchors.go)
- [internal/docrefs/references.go](../../internal/docrefs/references.go)
- [internal/docrefs/references_test.go](../../internal/docrefs/references_test.go)
- [internal/docsource/comments.go](../../internal/docsource/comments.go)
- [internal/docsource/document.go](../../internal/docsource/document.go)
- [internal/docsource/inputs.go](../../internal/docsource/inputs.go)
- [internal/docsource/source.go](../../internal/docsource/source.go)
- [internal/docsource/source_test.go](../../internal/docsource/source_test.go)
- [internal/doctor/doctor.go](../../internal/doctor/doctor.go)
- [internal/doctor/doctor_test.go](../../internal/doctor/doctor_test.go)
- [internal/frameworkinfo/build.go](../../internal/frameworkinfo/build.go)
- [internal/frameworkinfo/build_test.go](../../internal/frameworkinfo/build_test.go)
- [internal/generate/generate.go](../../internal/generate/generate.go)
- [internal/generate/generate_diagnostics_test.go](../../internal/generate/generate_diagnostics_test.go)
- [internal/retirement/denied.json](../../internal/retirement/denied.json)
- [internal/retirement/document_owners.go](../../internal/retirement/document_owners.go)
- [internal/retirement/document_owners_test.go](../../internal/retirement/document_owners_test.go)
- [internal/retirement/document_rules.json](../../internal/retirement/document_rules.json)
- [internal/safeerror/typed_source_contract_test.go](../../internal/safeerror/typed_source_contract_test.go)

## Integration files

- [docs/compatibility.md](../../docs/compatibility.md)
- [docs/evidence/guide-proof-pilot-20261107.md](../../docs/evidence/guide-proof-pilot-20261107.md)
- [docs/guides/audit.md](../../docs/guides/audit.md)
- [docs/guides/client-contracts.md](../../docs/guides/client-contracts.md)
- [docs/guides/client-forms.md](../../docs/guides/client-forms.md)
- [docs/guides/developer-tooling-and-testing.md](../../docs/guides/developer-tooling-and-testing.md)
- [docs/guides/http-application-errors.md](../../docs/guides/http-application-errors.md)
- [docs/guides/http-endpoints.md](../../docs/guides/http-endpoints.md)
- [docs/guides/http-model-binding.md](../../docs/guides/http-model-binding.md)
- [docs/guides/http-pagination.md](../../docs/guides/http-pagination.md)
- [docs/guides/model-declarations.md](../../docs/guides/model-declarations.md)
- [docs/guides/model-hooks.md](../../docs/guides/model-hooks.md)
- [docs/guides/model-mutators.md](../../docs/guides/model-mutators.md)
- [docs/guides/model-projections.md](../../docs/guides/model-projections.md)
- [docs/guides/model-queries.md](../../docs/guides/model-queries.md)
- [docs/guides/model-relations.md](../../docs/guides/model-relations.md)
- [docs/guides/model-writes.md](../../docs/guides/model-writes.md)
- [docs/guides/v0.2-migration.md](../../docs/guides/v0.2-migration.md)
- [docs/guides/validation.md](../../docs/guides/validation.md)
- [internal/cmd/checkdocs/main.go](../../internal/cmd/checkdocs/main.go)
- [internal/cmd/checkdocs/navigation_test.go](../../internal/cmd/checkdocs/navigation_test.go)
- [internal/docsource/document.go](../../internal/docsource/document.go)
- [internal/docsource/source_test.go](../../internal/docsource/source_test.go)
- [internal/retirement/docs.go](../../internal/retirement/docs.go)
- [internal/retirement/docs_test.go](../../internal/retirement/docs_test.go)

## Milestone 1 file inventory

- [CHANGELOG.md](../../CHANGELOG.md)
- [Makefile](../../Makefile)
- [blueprint/goway-data-layer/compilefail-mapping.tsv](../../blueprint/goway-data-layer/compilefail-mapping.tsv)
- [docs/evidence/guide-proof-pilot-20261107.md](../../docs/evidence/guide-proof-pilot-20261107.md)
- [docs/guides/account-recovery.md](../../docs/guides/account-recovery.md)
- [docs/guides/attachments.md](../../docs/guides/attachments.md)
- [docs/guides/audit.md](../../docs/guides/audit.md)
- [docs/guides/authentication.md](../../docs/guides/authentication.md)
- [docs/guides/client-contracts.md](../../docs/guides/client-contracts.md)
- [docs/guides/client-forms.md](../../docs/guides/client-forms.md)
- [docs/guides/developer-tooling-and-testing.md](../../docs/guides/developer-tooling-and-testing.md)
- [docs/guides/encryption.md](../../docs/guides/encryption.md)
- [docs/guides/generated-configuration.md](../../docs/guides/generated-configuration.md)
- [docs/guides/http-application-errors.md](../../docs/guides/http-application-errors.md)
- [docs/guides/http-cursor-pagination.md](../../docs/guides/http-cursor-pagination.md)
- [docs/guides/http-downloads.md](../../docs/guides/http-downloads.md)
- [docs/guides/http-endpoints.md](../../docs/guides/http-endpoints.md)
- [docs/guides/http-model-binding.md](../../docs/guides/http-model-binding.md)
- [docs/guides/http-pagination.md](../../docs/guides/http-pagination.md)
- [docs/guides/http-presence.md](../../docs/guides/http-presence.md)
- [docs/guides/model-accessors.md](../../docs/guides/model-accessors.md)
- [docs/guides/model-declarations.md](../../docs/guides/model-declarations.md)
- [docs/guides/model-extension-slots.md](../../docs/guides/model-extension-slots.md)
- [docs/guides/model-extensions.md](../../docs/guides/model-extensions.md)
- [docs/guides/model-hooks.md](../../docs/guides/model-hooks.md)
- [docs/guides/model-mutators.md](../../docs/guides/model-mutators.md)
- [docs/guides/model-pagination.md](../../docs/guides/model-pagination.md)
- [docs/guides/model-projections.md](../../docs/guides/model-projections.md)
- [docs/guides/model-queries.md](../../docs/guides/model-queries.md)
- [docs/guides/model-references.md](../../docs/guides/model-references.md)
- [docs/guides/model-relations.md](../../docs/guides/model-relations.md)
- [docs/guides/model-scaffolding.md](../../docs/guides/model-scaffolding.md)
- [docs/guides/model-writes.md](../../docs/guides/model-writes.md)
- [docs/guides/notifications.md](../../docs/guides/notifications.md)
- [docs/guides/password-hashing.md](../../docs/guides/password-hashing.md)
- [docs/guides/scoped-model-binding.md](../../docs/guides/scoped-model-binding.md)
- [docs/guides/sessions.md](../../docs/guides/sessions.md)
- [docs/guides/tokens.md](../../docs/guides/tokens.md)
- [docs/guides/trigram-search.md](../../docs/guides/trigram-search.md)
- [docs/guides/validation-expanded.md](../../docs/guides/validation-expanded.md)
- [docs/guides/validation.md](../../docs/guides/validation.md)
- [internal/cmd/checkdocs/main.go](../../internal/cmd/checkdocs/main.go)
- [internal/cmd/checkdocs/main_test.go](../../internal/cmd/checkdocs/main_test.go)
- [internal/cmd/renderdocs/main.go](../../internal/cmd/renderdocs/main.go)
- [internal/cmd/renderdocs/main_test.go](../../internal/cmd/renderdocs/main_test.go)
- [internal/docsource/document.go](../../internal/docsource/document.go)
- [internal/docsource/inputs.go](../../internal/docsource/inputs.go)
- [internal/docsource/repository_test.go](../../internal/docsource/repository_test.go)
- [internal/docsource/source.go](../../internal/docsource/source.go)
- [internal/docsource/source_test.go](../../internal/docsource/source_test.go)
- [internal/testinputs/fingerprint_test.go](../../internal/testinputs/fingerprint_test.go)
- [tests/fixtures/consumer/articles/http.go](../../tests/fixtures/consumer/articles/http.go)
- [tests/fixtures/consumer/authenticating/composed_routes.go](../../tests/fixtures/consumer/authenticating/composed_routes.go)
- [tests/fixtures/consumer/authenticating/sessions.go](../../tests/fixtures/consumer/authenticating/sessions.go)
- [tests/fixtures/consumer/clientcontracts/contracts_test.go](../../tests/fixtures/consumer/clientcontracts/contracts_test.go)
- [tests/fixtures/consumer/clientcontracts/member_create.go](../../tests/fixtures/consumer/clientcontracts/member_create.go)
- [tests/fixtures/consumer/clientcontracts/testdata/error-details.ts](../../tests/fixtures/consumer/clientcontracts/testdata/error-details.ts)
- [tests/fixtures/consumer/clientcontracts/testdata/fetch-transport.ts](../../tests/fixtures/consumer/clientcontracts/testdata/fetch-transport.ts)
- [tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts](../../tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts)
- [tests/fixtures/consumer/clientcontracts/testdata/surfaces.ts](../../tests/fixtures/consumer/clientcontracts/testdata/surfaces.ts)
- [tests/fixtures/consumer/clientcontracts/testdata/table-bindings/types.ts](../../tests/fixtures/consumer/clientcontracts/testdata/table-bindings/types.ts)
- [tests/fixtures/consumer/clientcontracts/testdata/table-bindings/typescript-inputs.json](../../tests/fixtures/consumer/clientcontracts/testdata/table-bindings/typescript-inputs.json)
- [tests/fixtures/consumer/clientcontracts/typescript-inputs.json](../../tests/fixtures/consumer/clientcontracts/typescript-inputs.json)
- [tests/fixtures/consumer/configured/settings.go](../../tests/fixtures/consumer/configured/settings.go)
- [tests/fixtures/consumer/httpendpoints/guide.go](../../tests/fixtures/consumer/httpendpoints/guide.go)
- [tests/fixtures/consumer/httpmodels/bindings.go](../../tests/fixtures/consumer/httpmodels/bindings.go)
- [tests/fixtures/consumer/httppagination/authenticated.go](../../tests/fixtures/consumer/httppagination/authenticated.go)
- [tests/fixtures/consumer/httppagination/cursor.go](../../tests/fixtures/consumer/httppagination/cursor.go)
- [tests/fixtures/consumer/httppagination/pagination.go](../../tests/fixtures/consumer/httppagination/pagination.go)
- [tests/fixtures/consumer/httpvalidation/async.go](../../tests/fixtures/consumer/httpvalidation/async.go)
- [tests/fixtures/consumer/internal/clientfixture/native.go](../../tests/fixtures/consumer/internal/clientfixture/native.go)
- [tests/fixtures/consumer/nestedbindings/bindings.go](../../tests/fixtures/consumer/nestedbindings/bindings.go)
- [tests/fixtures/consumer/notifying/orders.go](../../tests/fixtures/consumer/notifying/orders.go)
- [tests/fixtures/consumer/passwords/tables.go](../../tests/fixtures/consumer/passwords/tables.go)
- [tests/fixtures/consumer/profiles/profile.go](../../tests/fixtures/consumer/profiles/profile.go)
- [tests/fixtures/consumer/profiles/tables.go](../../tests/fixtures/consumer/profiles/tables.go)
- [tests/fixtures/consumer/reporting/table_routes.go](../../tests/fixtures/consumer/reporting/table_routes.go)
- [tests/fixtures/consumer/reporting/ticket_route.go](../../tests/fixtures/consumer/reporting/ticket_route.go)
- [tests/fixtures/consumer/reports/guide.go](../../tests/fixtures/consumer/reports/guide.go)
- [tests/fixtures/consumer/reports/rows.go](../../tests/fixtures/consumer/reports/rows.go)
- [tests/fixtures/consumer/scalarqueries/text_row_test.go](../../tests/fixtures/consumer/scalarqueries/text_row_test.go)
- [tests/fixtures/consumer/testdata/compilefail/guide_attachment_collection_replace_file/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/guide_attachment_collection_replace_file/invalid.go)
- [tests/fixtures/consumer/testdata/compilefail/guide_attachment_slot_add/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/guide_attachment_slot_add/invalid.go)
- [tests/fixtures/consumer/testdata/compilefail/guide_attachment_translation_rule/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/guide_attachment_translation_rule/invalid.go)
- [tests/fixtures/consumer/testdata/compilefail/guide_metadata_translation_rule/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/guide_metadata_translation_rule/invalid.go)
- [tests/fixtures/consumer/trigramqueries/search.go](../../tests/fixtures/consumer/trigramqueries/search.go)
- [tests/fixtures/consumer/types_test.go](../../tests/fixtures/consumer/types_test.go)
- [tests/fixtures/consumer/validationrules/presence.go](../../tests/fixtures/consumer/validationrules/presence.go)
- [tests/fixtures/consumer/vault/entry.go](../../tests/fixtures/consumer/vault/entry.go)
- [tests/fixtures/goway/guideadapters/auth_test.go](../../tests/fixtures/goway/guideadapters/auth_test.go)
- [tests/fixtures/goway/guideadapters/examples_test.go](../../tests/fixtures/goway/guideadapters/examples_test.go)
- [tests/fixtures/goway/guideadapters/scaffold_test.go](../../tests/fixtures/goway/guideadapters/scaffold_test.go)
- [tests/fixtures/goway/guideadapters/slots_test.go](../../tests/fixtures/goway/guideadapters/slots_test.go)
- `tests/fixtures/goway/guideadapters/snippets_test.go` (removed)
- [tests/fixtures/goway/migration/examples_test.go](../../tests/fixtures/goway/migration/examples_test.go)
- [tests/fixtures/goway/migration/guide_test.go](../../tests/fixtures/goway/migration/guide_test.go)
- [tests/fixtures/goway/migration/lifecycle_test.go](../../tests/fixtures/goway/migration/lifecycle_test.go)
- [tests/fixtures/goway/models/extensions/models.go](../../tests/fixtures/goway/models/extensions/models.go)
