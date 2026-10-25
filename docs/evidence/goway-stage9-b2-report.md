# Stage 9 B2

`lifecycle.CountObservers[M](Observers) RegistrationCounts` exposes only integer
`Write`, `Retrieval` and `DeletionOnly` counts for the exact model. It does not invoke
callbacks or expose adapters, factories or capabilities. The deletion view reports
its merged registrations. External negatives reject passing counts to the factory
lookup. No ORM Query/Table/column methods or surface snapshot entries were added.

## Package status

| Package | Status and preserved assertions |
| --- | --- |
| observerqueries | ORM cutover complete; one observer family per model/application; count inspection after row closure; ordering, capture, veto, cancellation and concurrency. |
| hookqueries | ORM cutover complete; per-write factories, locked originals, defaults, transactional logs, savepoints, after-commit identity/redaction. |
| timequeries | ORM cutover complete; managed roles and opt-out, clock ownership/failure isolation, one sample per batch, trigger RETURNING, upserts. |
| softqueries | ORM cutover complete; tokens, visibility, restore/force-delete ordering, relations, aggregates, joins, DTOs, bounded keys and factories. |
| tenantqueries | ORM cutover complete; lazy request scopes, fail-closed missing context, explicit bypasses, scoped writes/upserts/lookups, pivots and pruning. |
| auditqueries | ORM cutover complete; instance-owned registration, stored identities/codecs, disclosure policies, historical fields and transactional rollback. |
| retrievalqueries | Declarations and ordinary/composed reads publish-ready; model streaming migrated. Complete-record streaming remains blocked as described below. |

Generated directives, outputs and ownership manifests remain for the lead's barrier.
B1 changes remain intact underneath B2. B2 changes 54 files across these packages
(audit 7, hooks 7, observers 5, retrieval 10, soft deletes 10, tenancy 8, timestamps 7),
plus 33 shared-owner/catalogue/compiler/probe files. It adds 15 table declarations
and two DTO row declarations.

## Lane C request

`rows.RecordRead[S,M]` needs complete-model streaming equivalent to legacy
`query.RecordSelection.Each`: selected window/order, bounded closed batches when
retrieval callbacks exist, one local and observer factory per nonempty batch,
callbacks finished before yielding, early-stop identity and row closure. Current
`rows.Each` accepts DTO `rows.Query[S,R]` only and skips model retrieval dispatch.
The CTE assertion remains in `retrievalqueries/legacy_iteration_postgres_test.go`;
its separate application registers the legacy member family only. The ORM model
stream test retains the same assertions. No raw-SQL or eager-All substitute was added.

## Catalogue and footprint

Twenty existing consumer cases now use ORM, plus new `observer_counts_adapters`.
The three formerly pending Stage 7 audit rows (`audit_field_value`,
`audit_history_id`, `audit_model_owner`) now have independent Go-way negatives.
Catalogue: 538 ported, 45 pending, 444 unaffected; 1,027 total.

| Probe | Before | After | Ceiling |
| --- | ---: | ---: | ---: |
| q_with | 1,211 | 1,211 | 1,211 |
| aggregate_families | 4,366 | 4,366 | 4,369 |
| observer_counts | new | 63 | 73 |

Existing ceilings are unchanged. New ceiling is ceil(63 × 1.15).

## Shared files

`database/lifecycle/{counts.go,counts_test.go,README.md}`;
`blueprint/goway-data-layer/compilefail-mapping.tsv`;
`tests/fixtures/consumer/types_test.go`;
`tests/fixtures/goway/{compiler_cases_test.go,footprint_test.go}`.
Only lane B entries were changed in the shared catalogues/registries.

## Docs notes

Consumers can inspect `lifecycle.CountObservers[Record](rows.Observers())` after
closure without knowing the private adapter family. B2 fixtures publish explicit
ORM declarations, typed roles, relation descriptors and DTO rows; their package
READMEs document names and migration boundaries. Choose `auditqueries.ORMDomain()`
instead of the legacy domain when assembling ORM applications. Do not register
both adapter families for a model. Generated cleanup and manifests remain lead-owned;
no persisted schema or wire declaration changed.

Verification details and current source hashes are in `goway-stage9-b2.json`.
PostgreSQL assertion sources compile; execution remains with the lead.
