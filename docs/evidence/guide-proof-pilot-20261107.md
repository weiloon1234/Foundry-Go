# Guide/proof pilot and pinned documentation integration

Milestones 1 and 2 integration handoff, 2026-11-07, on `lane/s6-guides`.
The lead replayed milestone 2 (`6a0162de`) onto milestone 1 (`228d5403`).
This integration changes documentation checks and handwritten prose only; fixture
sources, generated fences, CLI version selection, packaging and Makefile ownership
remain as delivered by those milestones.

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

## Acceptance mapping

| Brief item | Implementation |
| --- | --- |
| Region parser and exact rendering | [internal/docsource](../../internal/docsource) parses standalone or trailing boundary comments, validates unique region names, rejects overlap, removes only markers and common indentation, selects a safe delimiter and derives the language. Fragmentless directives select the whole file. |
| Generation ownership | [renderdocs](../../internal/cmd/renderdocs) renders or compares; [Makefile](../../Makefile) calls it after declaration generation in `generate` and read-only `generate-check`. |
| Documentation checks | [checkdocs](../../internal/cmd/checkdocs) delegates equality, unique owning inclusion, approved source roots, complete-guide coverage, recipe fence binding and navigation-only entries to docsource. Explicit anchor indexing and `agent-entry`/`recipe-*` link checks now delegate to docrefs; Agent entry line locations come from the same docsource section parser. |
| Existing error examples | Both Go declarations and TypeScript narrowing now name explicit regions. The TypeScript create-member behavior is unchanged. |
| Pilot guides | Existing owners have task navigation and explicit `agent-entry` and `recipe-*` anchors. The table below records the teaching reconciliation. |
| Compilation proof | `fixture-check` collects actual Go files selected by `go list -json ./...` in its fixture loop. Docsource compares the directive inventory against those inputs, including test files and excluding ignored files. TS examples share the actual strict compiler lists with the current-client exporters. |
| Required TypeScript | `verify` requires the selected tool paths and exports `FOUNDRY_TEST_TYPESCRIPT_REQUIRED=1` to its fixture run. The existing CI environment remains unchanged. No second TypeScript acceptance run is added to verify. |
| Tests and authoring | Renderer, read-only comparison, malformed markers, ownership, paths, section coverage and input-list tests are written. Four receiver-family compiler negatives and a conditional error-export negative were added. The [authoring workflow](../guides/developer-tooling-and-testing.md#author-compiled-guide-examples) and [Unreleased changelog](../../CHANGELOG.md) describe the change. |
| Legacy migration | Every existing compiled-source directive names an explicit region. The old guideadapters substring test is removed; its complete-guide policy and repository test now live in docsource. The separate historical ORM migration census still checks its legacy examples, handing source-bound fence equality to docsource. |
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
