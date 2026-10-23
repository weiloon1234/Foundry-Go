# Stage 5b relation writes

Updated: 2026-10-23T16:01:55+00:00

Implemented on `80546f4`; no commit. PostgreSQL smoke sources were compiled and
vetted only. The [evidence JSON](goway-stage5b-relation-writes.json) records commands,
measurements, corrections and current source hashes.

## Accepted review fixes

Nullable relation predicates use the stored codec, including keys whose mutator
input is a different type. A missing distinct-input pivot key or morph discriminator
is rejected before pivot hooks or mutation SQL, matching both old attachment paths'
FoundryCreateMutation boundary. Explicit typed inputs and hook replacements still
transform exactly once; hook getters see Nullable[I], never a substituted Nullable[V].
The erroneous Stored bypass and its shared querycore changes have been removed.
Single Attach uses the old source-scoped EXISTS postcondition after pivot hooks,
checking both original endpoint identities, their scopes and the stored link in the
same transaction. Its primary-key statement count remains four. Write-row limit
failures return `fault.Invalid`; endpoint cardinality failures retain
`database.TooManyRows`. All three `PivotChanges` fields now have concrete examples
and `Detached` documents soft versus physical removal.

Regressions cover omitted and partial distinct inputs, explicit inputs, typed hook
getters/replacements, mutator rejection, unchanged same-type defaults, and no-create
operations. They exercise Attach, AttachMany, Sync, SyncWithoutDetaching and Toggle,
with exact statement counts and no INSERT or callbacks on missing-input failures.
Both endpoint postconditions, row limits and cardinality remain covered. An exact single-Attach old/new golden
joins the seven existing bulk goldens. New PostgreSQL sources assert that source
and target soft-deletion hooks roll back both endpoint and pivot changes.

## API

Package functions: `Attach`, `AttachMany`, `Detach`, `ForceDetach`, `DetachMany`,
`DetachAll`, `Sync`, `SyncWithoutDetaching`, `Toggle`, `UpdatePivot` and
`WithRelationWriteLimit`. Inputs use `ThroughRelation[M,N,P]`, concrete endpoints
and `Assignment[P]`; sync takes separate create/update slices (nil update skips
retained-link updates). `PivotChanges[P]` exposes `Attached`, `Detached`, `Updated`.
`MaxRelationWriteRows` reuses the existing atomic write bound.

Endpoint keys and concrete morph discriminators fill omitted inputs only when
stored and input types match. Stored keys
are refreshed under locks; scopes, filters, visibility and cardinality checks
remain active. Bulk operations serialize current-link changes with a source
`FOR NO KEY UPDATE` lock. All per-model writes reuse B5b hooks, observers, mutators,
conventions, change capture and after-commit ownership. An operation owns one
transaction/savepoint, with no savepoint per pivot. Unknown outcomes return typed
`WriteError[P]`, `WriteError[[]P]` or `WriteError[PivotChanges[P]]` candidates; they
never become successful results or automatic retries.

Without pivot hooks/observers, mutations are set-based. Attachment costs four
statements for a nonempty batch; sync/update/toggle/detach-many add one current-link
read and one statement per nonempty mutation kind. Each insertion adds its stored
postcondition query. Single detach/force-detach cost three statements without
callbacks (the requested set-based behavior); hooked removal selects candidates
and runs each normal lifecycle. Eight SQL/binding/result goldens (seven bulk, one single Attach) match the
old API exactly. Regression tests cover observer-only/deletion-only models,
refreshed non-primary keys, limits, filtered postconditions, cancellation,
savepoint/after-commit ownership, failed callbacks and typed unknown outcomes.

## Catalogue and surface

Ported only `relation_attach_draft`, `relation_attach_source`,
`relation_attach_target`, `relation_create_defaults_owner`, `relation_detach_result`.
Generated draft/default ownership maps to pivot-owned assignments and create slices.
The fixture has 357 compiler cases.

The surface snapshot adds 16 lines: the 11 functions above, value/pointer entries
for `PivotChanges`, and its three fields. No existing signature or Query/Table/
relation descriptor method changed. No new capability bridge or dependency.

## Footprint

| Probe | Functions | Ceiling |
| --- | ---: | ---: |
| relation_write_declaration | 361 | 416 |
| relation_write_attach | 1,110 | 1,277 |
| relation_write_sync | 1,124 | 1,293 |
| q_with | 1,209 | 1,211 |
| aggregate_families | 4,362 | 4,369 |

All 63 probes pass. Ceilings retain the original `ceil(measured × 1.15)` budgets; no ceiling
was changed during review fixes. Both tight probes have zero growth. New generic work is reached
only by package function calls; shared selection/mutation work is non-generic.
Shared column metadata retains its stored-codec pointer for predicates and typed
default capture. There is no assignment marker or mutator bypass. Public handles
and method sets remain unchanged; existing footprint ceilings are retained.

## Acceptance

All final checks passed with `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2`,
`GOMAXPROCS=4`, `GOFLAGS=-p=4` and `GOWORK=off`:

- `go build ./...`
- `go vet ./database/... ./internal/...`
- `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...`
- `go test -race ./internal/generate -run '^TestRelationWriteDefaultsPreserveUUIDAndDistinctInputs$'` (the old-API default-typing oracle).
- Fixture `go vet ./...`; `go test -v` on every fixture package except pgsmoke.
- Fixture `go test ./pgsmoke -run '^$'` (compile only).
- `python3 tools/goway_catalogue.py`, `gofmt`, `make docs-check`, `git diff --check`.

Historical implementation failures and corrections remain in the evidence.
The review gate passed on its first batch, including the full requested race scope
after removing the mutator bypass. PostgreSQL regression sources were vetted
and compiled; live execution remains with the lead.

## Shared files

Review fixes retain changes to `database/orm/column.go` and `declaration.go` for
stored-codec binding. `internal/querycore/mutation.go`, `field_mutator.go` and
`field_mutator_test.go` are restored to their branch-base contents; no shared mutator
bypass remains. The public surface and catalogue rows are unchanged by these fixes.

- `database/orm/write.go`: extract the shared transaction body, bounded set RETURNING,
  and defer identity checks until relation candidate selection (final per-model
  compilation still requires a primary-key equality).
- `database/orm/relation.go`, `relation_join_plan.go`, `relation_morph.go`: private
  write limit and immutable concrete morph defaults.
- `database/orm/README.md`, `method_surface_test.go`, `testdata/public-methods.txt`.
- `tests/fixtures/goway/compiler_cases_test.go`, `footprint_test.go`.
- `blueprint/goway-data-layer/compilefail-mapping.tsv`: only the five assigned rows.

New implementation, regression, consumer/compiler, probe and pgsmoke files are
listed and hashed in the evidence.

## Docs notes

Lead-owned files were not edited. Record Stage 5b relation-write completion,
package-function syntax, typed reconciliation candidates and the lifecycle/set-based
selection rule. The five remaining Stage-4-labelled catalogue rows are now ported
under Stage 5b. Record 357 compiler cases, 63 passing footprint probes, unchanged
existing ceilings and scoped acceptance; retain live PostgreSQL as lead-owned work.

Review docs notes: mention stored-key support for distinct nullable inputs,
rejection of omitted distinct inputs before hooks or mutation SQL, post-hook
revalidation of both single-Attach endpoints, `fault.Invalid` for atomic
write limits and the documented soft/physical detached-result semantics. No
additional public methods, catalogue rows or raised footprint ceilings.
