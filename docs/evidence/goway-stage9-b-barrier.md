# Stage 9 lane B cleanup barrier

Base: `0ada5ac7`. Changes are confined to lane B's consumer packages, the owned
`outbox_field_id` negative, generated ownership updates, and this evidence.

## Removed

- 36 model/projection directives in 15 packages: auditqueries, binarymodels,
  eventqueries, hookqueries, inputqueries, linkqueries, observerqueries,
  retrievalqueries, softqueries, tenantqueries, timequeries, upsertqueries, vault,
  recovering and nestedbindings.
- `make generate` removed 36 obsolete generated files and refreshed 15
  ownership manifests. It also removed obsolete managed field notices. DTO, enum,
  config and contract directives remain. No wire/schema output was changed.
- Legacy hook, retrieval, relation and scope factories, generated draft/change
  instrumentation, and linkqueries' alternate legacy observer setup are removed.
  The hook recorder still supplies the native callback/log/veto behavior, with
  its unused generated-changes parameter removed.
- `outbox_field_id` now rejects the wrong payload ID through
  `eventqueries.Links.MessageID.Set`, preserving its expected `cannot use` diagnostic.

## Preserved assertions

- Link read SQL and bindings are frozen in
  [legacy-read.json](../../tests/fixtures/consumer/linkqueries/testdata/legacy-read.json),
  captured through the old API at this base before output removal. The existing
  `TestORMDeclarationsAndReadParity` compares the native statement with that capture.
- `TestPostgresLegacyCreationAdapterIsLazy` is obsolete because its assertion
  concerns the removed application-implementable `FoundryCreateMutation` bridge.
  ORM lookup accepts sealed assignments instead. The retained consumer
  `TestPostgresLookupWritesChooseOneLifecycleBranch` and framework
  `TestLookupExistingSkipsUnchosenDraftAndRetrieval` cover found-branch laziness,
  unused invalid input and ordinary creation behavior. No PostgreSQL behavior
  assertion is claimed to have run in this service-free lane.
- Observer registration still rejects an incompatible hook family, without invoking
  factories. That assertion now supplies a foreign typed family directly instead
  of constructing generated model adapters.
- Recovery Member and nested Team retain handwritten `FoundryIdentity` methods
  backed by their ORM declarations. New service-free tests check the original
  stored model name and key codec and enforce `model.Identifiable` statically.
- Complete-record DISTINCT ON cursor/page assertions were already migrated in this
  base. Their bodies are unchanged; stale legacy file/test names are removed.

## Blockers: lane D

`httppagination/pagination.go:70–73` still returns `mutatorqueries.MemberQuery` and
calls `QueryMutatorMembers` and `MemberFields`. Therefore the mutatorqueries model
annotation, generated model output and managed notices remain. Lane D must port
that source to its accepted ORM declaration before B can remove them. Its enum
output remains required afterward.

`testdata/compilefail/reference_natural_key/invalid.go:8` still calls
`(linkqueries.Group{}).FoundryReference().Key()`. The first consumer compiler run
correctly rejected removal as an unrelated undefined-symbol failure. Therefore
Group's original model directive, `groupReads` factory and generated output are
retained. Lane D must express that natural-key negative through the native identity
declaration before B removes them. No lane D source was edited.

Lane C also continues to own `models.User` identity when retiring its generated
output; B's service consumers still require that implicit auth contract.

## Base catalogue discrepancy

The brief states zero pending rows. Both HEAD and the current validated catalogue
have 11 pre-existing `port` rows: `auth_bound_resource_model`, `auth_bound_subject_model`, `auth_optional_bound_subject`, `http_model_binding_handler`, `http_model_binding_owner`, `http_model_binding_response`, `importer_column_row`, `reference_key_type`, `reference_model_owner`, `reference_natural_key`, `workflow_bound_model`.
Only the owned outbox source/rule changed in this slice. These pending rows belong
to lanes A, C and D under the roster; the cleanup does not reclassify them.

## Acceptance

The accompanying JSON records commands, outcomes, source hashes and the frozen
capture provenance. Build excludes the two test-only cursor/page packages; their
sources compile through tests and vet. PostgreSQL and Redis stay opt-in and were
not run. Existing footprint ceilings are unchanged. No commit was created.
