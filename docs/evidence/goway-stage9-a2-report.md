# Stage 9 lane A2

Base: fdaff938 (batch 7b), with accepted terminals and RelationFilter reapplied.
No commit. Generated outputs, directives, manifests, module pins and wire types
remain retained for the lead's generation barrier.

## Rebase

Resolved the five requested conflicts: integrated Stage 7 evidence uses HEAD;
the method surface and compiler/footprint registries retain both sides' additions.
The surface mismatch on the initial run was order-only: the 3,558 snapshot lines
were identical as multisets. Registry order was aligned with the retained snapshot.
Targeted rebase acceptance passed in segments: completed r8 commands are retained;
the interrupted fixture race and affected surface check passed on r9. No command
was restarted against an older cache.

## Packages

Runtime cutovers below are ready for integration; final directive/generated cleanup
remains at the lead-owned barrier. No package is claimed fully retired from generated
coexistence.

- `pivothooks`: ORM declarations, full retained pivot operation tests and
  CreateOrFirst conflict-provenance tests; local and provider callbacks use ORM
  hooks. Legacy declaration factories remain only for generated coexistence.
  Event recording, vetoes, nested-write callbacks and commit recording share
  their existing owner. No application registers both observer families.
- `relationshipfilters`: ORM EXISTS/NOT EXISTS, nested self and pivot predicates,
  eager loading, typed key reads and scoped updates; existing assertions retained.
- `productionprofile`: handwritten record declaration and thin Tables registration,
  model/scalar compile terminals and nullable self-join composition. Field
  benchmarks now exercise the corresponding expression types.
- `configuredprofile`: validates the record declaration through WithTables;
  existing application/service assertions retained. Its command and both
  productionprofile commands consume the migrated API without source changes.
- `bootstrap`: member/operator declarations and typed identities, ORM actor reads,
  fixture writes and profile feature references; WithTables validates thin handles.
  `bootstrap/cmd/serve` consumes the same Build entry point without source changes.
- `correlations`: native rows/expr composition, retained grouping/HAVING, nested
  outer references, scalar cardinality, cancellation, scoped writes and through
  filters. Compile-only old/new parity covers grouped scalar correlation and
  the expression relation filter. `correlations/nullable` was already migrated.
- `officequeries`: declarations, relation plans, aggregate slot and lazy desk
  global scope are publish-ready. Runtime cutover is blocked by the exact old
  `RelatedValue`/`WithValue` assertions at office_postgres_test.go:122–123 and
  171–180: ad-hoc relation aggregate selection, ordering and filtering without
  a model slot. The ORM currently exposes slot-bound Related. Those legacy
  assertions remain; no surrogate model slot, raw SQL or application-built
  relation subquery was introduced.

## Negatives and shared files

Six retained consumer negatives now use ORM declarations: relation_attach_draft,
relation_attach_source, relation_attach_target, relation_create_defaults_owner,
relation_detach_result and soft_pivot_scope. Their existing ported mapping rows
receive consumer-cutover notes; no disposition is relabelled. The four generic
inference diagnostics are updated in the consumer registry.

Other relation cases already use A1 declarations. `nested_field_owner` still
needs lane D's nestedbindings ORM declaration; its legacy negative remains.
Bootstrap actor/payload negatives retain their concrete identities unchanged.

Shared registry edits: database/orm/method_surface_test.go,
tests/fixtures/goway/compiler_cases_test.go, tests/fixtures/goway/footprint_test.go,
tests/fixtures/consumer/types_test.go, and this lane's six mapping rows.
No framework API additions beyond the already accepted terminals/RelationFilter.
No new footprint probes or ceiling changes in A2. The post-rebase footprint run
passed all budgets: q_with 1,211/1,211; aggregate_families 4,366/4,369;
read_record 1,002/1,153; read_scalar 1,165/1,340; read_set 1,050/1,208;
relation_filter 1,831/2,106. A2 adds only consumer sources outside these probes.

## Acceptance

Passed on r9 with GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off:

- Consumer build: officequeries, pivothooks, productionprofile/..., configuredprofile/..., bootstrap/... and correlations/.... The test-only relationshipfilters package is compiled by its tests.
- Vet, tests and race tests: the same packages plus relationshipfilters.
- The 31 selected relation/pivot/aggregate/bootstrap/startup compiler cases, including all six updated negatives.
- Catalogue, make fmt-check, make docs-check and git diff --check.

Initial failures were corrected without weakening assertions: SetNull replaces an
incorrect nullable assignment spelling; the office compile parity test supplies the
context required by ORM global scopes; explicit go build excludes the test-only
package. The two configuredprofile HTTP tests passed both ordinary and race reruns
outside the listener-restricted sandbox. Other passing package results were retained.

The accepted terminals/RelationFilter rebase gate also passed: database/orm/query/
ormcore build and vet, races (surface order corrected then rerun), six owned compiler
cases, all footprint budgets, fixture SQL parity/race tests and jsonqueries vet/tests/
races. The in-progress A2 catalogue note mismatch was corrected and the catalogue
passed afterward. The retained snapshot lost no entries. PostgreSQL smoke sources
also passed vet and race compilation with `-run ^$` on r9.

Live PostgreSQL/Redis and integration gates remain lead-owned. PostgreSQL assertions
are retained and compiled, not claimed as run. Generated outputs, manifests, module
pins, Makefile and contract declarations have no edits. Exact segmented commands,
results and source hashes are in goway-stage9-a2.json; logs are under
/private/tmp/goway-stage9-a2 and /private/tmp/goway-stage9-relation-filters/rebase7b*.

## Docs notes

Lead changelog: application fixtures now demonstrate thin table registration,
typed actor references, ORM pivot lifecycle operations and correlated relation
filters. Keep generated coexistence until the serial cleanup/generation barrier.

Requests: route ad-hoc relation aggregate values to their framework owner and
publish nestedbindings declarations before migrating nested_field_owner.
