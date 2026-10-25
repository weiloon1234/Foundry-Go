# Stage 9 lane D: cleanup barrier

Base `0ada5ac7`; the accepted zero-pending catalogue delta is preserved. No commit.
Original acceptance used r12. Review 27 reruns use r13 with
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

## Owned cleanup

Removed 14 model directives and one projection directive from `profiles`,
`articles`, `passwords`, `multifactor`, `idempotenthttp`, `isolatedhttp`,
`teamworkflow` and `nestedbindings`. `make generate` removed 16 obsolete outputs
and refreshed these eight ownership manifests. Retained DTO, enum, config,
localization and client generated files are byte-identical. No generator source,
legacy Go-way oracle, migration, dependency or TypeScript file changed.

Removed eight coexistence factories/helpers: articles' three relation/extension
factories; nestedbindings' two relation factories and retrieval factory;
teamworkflow's relation factory; and passwords' `AccountDraftWithHash`.
Article policy values moved unchanged into `ArticleSlots`. Password creation,
compare-and-swap, login locks, rehash, projection and audit assertions now consume
ORM declarations. Password/MFA accounts and nested teams retain handwritten
`FoundryIdentity` adapters for their existing authentication contracts.
`httppagination` uses ORM queries and pagination types.

Seven independent snapshots retain the legacy SQL/bindings, wire identities and
extension metadata captured **before** removal. The capture program, source hashes
and snapshot hashes are in `goway-stage9-d-barrier-freeze.json`. Existing assertions
consume these records; no expected output is recomputed from the new declaration.

## Harness and editor

Seventeen retained root compiler programs now target handwritten APIs; their
catalogue rule cells and four diagnostic anchors follow the same typed failures.
All 77 selected owned/dependent negatives pass in a segmented run. The three keyed
lookup cases use `Table.Key()`, so rejection occurs at the intended wrong-owner or
locked-query boundary rather than an invalid key handle.

The mutator probe now anchors an actual handwritten consumer call. Bootstrap and
outbox model-field probes have separate concrete identity-adapter probes, replacing
their generated `FoundryReference` completion requirement. Extension documentation
still requires the localized-heading semantics and now names `ArticleSlots.Title`.
The new root tests assert normalization and concrete event-reference identity.

## Coordination blockers

Lane C must publish replacements for `models.User.CurrentEmailDraft` in
`models/models.go` and `catalog.Product.CreatorDraft` in `catalog/product.go`.
The root callers remain intact at `generated_test.go:109` and
`package_graph_test.go:45`: they assert the business helpers' actual supplied email
and typed creator key. Substituting direct assignments would lose that coverage.
Lane C is supplying owner-typed assignments for both helpers. Once integrated,
the root tests will consume the actual returned assignments with
`inspectInsertAssignments`, checking presence and the exact value, including an
empty email. No other owner's source package was edited.

The catalogue validator passes with 587 ported, 445 unaffected, **zero pending**.
Seven of eight catalogue unit tests pass. The remaining summary test is blocked
by the lead-owned blueprint README's old totals; its assertion remains intact.
The exact replacement summary is already in the accepted final-catalogue report.

## Acceptance

Command results and hashes are recorded in `goway-stage9-d-barrier.json`.
The r12 results below predate the final import regrouping: review 27 found that
it removed `cleanupfixture` imports from the three same-package fixture tests.
Those earlier test/vet passes did not establish acceptance of the submitted files.
The imports are restored without changing their frozen SQL/bindings assertions;
the r13 correction entries in the JSON replace that affected evidence.
`go test -race -count=1 -v ./idempotenthttp ./isolatedhttp ./teamworkflow` and
full consumer `go vet ./...` pass after the final imports and gofmt. The three
changed files match their pre-verification hashes. PostgreSQL opt-in and process
child tests are skipped; no live database verification is claimed.
Framework/consumer build, consumer/agent vet, owned package and root races,
77 compiler cases, catalogue validation and generation-check pass. PostgreSQL
sources compile and vet; live PostgreSQL/Redis integration is reserved for the lead.
All 502 real gopls probes pass, including the new identity probes and preserved
nullable-clearing and extension-documentation assertions. The final slot-comment
probe passes again after correcting its explicit-name and Relation adapter guidance.
Docs-check, gofmt and diff-check pass. Initial compilation caught and corrected the article-owner
and nested-team identity dependencies; no assertions were removed.

## Surface, footprint and shared files

No framework API or method-surface addition; no footprint probe or ceiling changed.
Shared files touched by this cleanup: consumer README, root `types_test.go`, new
root `model_adapter_editor_test.go`, both editor probe files, the catalogue's
17 source-rule cells, and the eight generated ownership manifests. The prior
accepted catalogue tool, registry and ten new negatives remain in the cumulative
diff. The JSON lists exact changed paths and deleted outputs.

## Docs notes

The lead can record lane D's eight owned packages as past the cleanup barrier,
with the two lane C business-helper contracts still requiring coordinated root
assertion updates. Preserve the frozen expectations through Stage 8c. The
catalogue README summary correction remains lead-owned.
