# Stage 9 B3 — typed unset and consumer cutovers

Built on the accepted B1+B2 work at `6d80111`; those changes are retained. No commits or git writes.

## API and behavior

```go
func orm.WriteUnset[M, I any](write *orm.Write[M], setter func(I) orm.Assignment[M]) error
```

`orm.WriteUnset(write, Fields.EmailRevision.Set)` removes the pending assignment.
Required zero and nullable NULL both become omitted. No mutator or encryption input
is left for that field; database defaults and automatic conventions still apply.
It shares getter validation, rejects foreign declarations/expired drafts, and keeps
errors sticky even when a hook ignores them. The operation uses the existing
operation-owned assignment slice; caller slices stay reusable. Tests cover SQL
omission, `Assigned`, stored snapshots, double-unset, reassignment, mutator counts,
foreign/nil/expired access and a wrong-model compiler negative.

Framework surface additions: `orm.WriteUnset`, `orm.RestoreEach`, and
`orm.ForceDeleteEach` (snapshot updated and matched).
No Query/Table/column methods or capability bridges were added.

## Consumer status

| Package | B3 handwritten files | Status |
| --- | ---: | --- |
| `binarymodels` | 7 | Migrated: input/storage bytes, owned draft buffers, stored changes and explicit rows DTO. |
| `eventqueries` | 8 | Migrated: one ORM observer family, transactional outbox and after-commit events; typed message-ID table. |
| `hookqueries` | 3 | Review: exact caller-input inspection before hook defaults. |
| `observerqueries` | 3 | Review: exact raw-input inspection before observer transformations. |
| `inputqueries` | 5 | Migrated CRUD/conflicts; typed live getters/unset and explicit accessors. Legacy generator factory retained. |
| `linkqueries` | 16 | Source writes, lookup branches/concurrency, pivots and per-model update locking migrated. Restore/force-delete batches now use the shared ORM engine. |
| `mutatorqueries` | 4 | Migrated CRUD/conflict/reference callers; stored identity and explicit accessor assertions retained. |
| `upsertqueries` | 5 | ExcludedRecords publish-ready and its tests migrated; shared-model scenarios blocked on lane C. |
| `vault` | 4 | Migrated encrypted CRUD/bulk/conflicts, rotation, envelope binding and redaction assertions. |

Four tables and one DTO declaration are newly published. The existing model, event,
DTO and schema fields are unchanged. `Records.RawBytes` binds the model's `Raw`
field without shadowing `Table.Raw`. Generated outputs, ownership manifests,
module files, the Makefile and wire artifacts were not edited.

## Rows and footprint

Ten consumer negatives now use ORM/rows: six `binary_*` and four `encrypted_*`.
New `write_unset_owner`, `each_restore_owner`, `each_force_owner`,
`each_force_token`, and `each_restore_result` negatives exist in both independent modules. Catalogue:
543 ported, 45 pending, 444 unaffected (1,032 total). All 82 lane-owned cases pass
in both consumer and Go-way modules.

| Probe | Before | After | Ceiling |
| --- | ---: | ---: | ---: |
| `q_with` | 1,211 | 1,211 | 1,211 |
| `aggregate_families` | 4,366 | 4,366 | 4,369 |
| Other 124 existing probes | unchanged | unchanged | unchanged |
| `write_unset` | new | 266 | 306 |
| `each_soft_writes` | new | 1,567 | 1,803 |

All 128 probes pass on Go 1.27.1 darwin/arm64. Counts are T/t symbols in package
archives, using the existing harness; 306 is `ceil(266 × 1.15)`. 1,803 is `ceil(1,567 × 1.15)` for the new restore/force-delete probe. All 127
pre-review counts remain unchanged; existing budgets were not raised. The JSON retains all measured probes and source hashes.

## Acceptance

Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r7 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.
PostgreSQL URL is empty and required mode is 0: PostgreSQL assertions compile but
were not run. The lead owns service/integration, generation and contract gates.

- PASS: `go build ./database/orm`; build of all seven consumer packages above.
- PASS: `go vet ./database/orm`; vet and ordinary tests of the seven consumers.
- PASS: `go test -race ./database/orm`; race tests of all seven consumers (segmented corrections recorded in JSON).
- PASS: `TestInvalidPublicContractsFailCompilation` with the 82 lane-owned names in each fixture module.
- PASS: Go-way `TestFootprintBudgets` (128 probes).
- PASS: catalogue, `make docs-check`, gofmt and `git diff --check`.

The interrupted r6 ORM race run was discarded and rerun on r7. Implementation
corrections included expression phase/type usage, an obsolete conflict method,
projection result adaptation and the binary field-handle collision. The review sweep restores exact input/omission checks (details below); the old public creation-adapter laziness oracle moved to a separate
legacy test, while ordinary lookup behavior now uses ORM.

## Shared files

- `database/orm/write_draft.go`
- `database/orm/write_each.go`
- `database/orm/column_role.go`
- `database/orm/method_surface_test.go`
- `database/orm/testdata/public-methods.txt`
- `database/orm/README.md`
- `blueprint/goway-data-layer/compilefail-mapping.tsv`
- `tests/fixtures/consumer/types_test.go`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/footprint_test.go`

The complete B3 file census and fingerprints are in the evidence JSON; edits are
confined to Lane B packages/rows and the approved shared draft owner.

## Requests and retained blockers

- Lane D: use `WriteUnset(write, Fields.EmailRevision.Set)` for recovering's revision removal once integrated; recovering was not edited here.
- Lane C: integrate `models.User`, `models.WriteRecord` and `internal/queryfixture` declarations/cutover for the remaining upsert scenarios. No duplicate shared declarations were introduced.
- Lane A: complete-record streaming remains assigned to you; the retrieval legacy assertion is untouched.
- Retain `legacy_creation_bridge_postgres_test.go` as an old public-interface oracle until the lead retires that bridge. ORM lookup callers use sealed assignments rather than an application creation adapter.

## Docs notes

Add `WriteUnset` to the lifecycle guide's draft API: omission is distinct from NULL
and zero; use it within before-write callbacks. Recovering no longer needs to
approximate removal by assigning the previous stored value. Record B3's four new
tables/one DTO and the consumer statuses above, without marking blocked packages
or generator retirement complete. No persisted schema or public wire change.

## B3 review correction and bounded soft-model batches

```go
func orm.RestoreEach[M any](ctx context.Context, writer database.Transactor,
    q orm.Query[M], soft orm.SoftDeletable[M], limit int) ([]M, error)
func orm.ForceDeleteEach[M any](ctx context.Context, writer database.Transactor,
    q orm.Query[M], soft orm.SoftDeletable[M], limit int) ([]M, error)
```

Both validate the declaration-owned token, then reuse `writeEach`. Restoration
selects deleted rows; force deletion retains the query's visibility. The shared
owner retains predicates, locked primary ordering, bounds before callbacks, row
closure, local hooks then observers, cancellation, atomic rollback and after-commit
work. Unknown commits never return successful rows or retry: typed `WriteError[[]M]`
retains reconciliation candidates. No new capability bridge or execution engine.

The two previously blocked link batch suites now use ORM, including natural keys.
New PostgreSQL regressions compare persisted models before/after bound failures,
later before/after-hook vetoes and cancellation; after-commit callbacks remain absent.
These sources compile; PostgreSQL execution remains the lead's gate.

The assertion sweep compared changed conditions and diagnostics against HEAD across
Stage 9 consumer tests. Six checks now inspect reused assignments before transforms:

- Insert-select: original tag, decimal, JSON and CreatedAt, with ID/UpdatedAt omitted.
- Pivot attach: original role, with derived keys, ID and UpdatedAt omitted.
- Hooks: original nickname, with Email, ID and IntroducerID omitted.
- Distinct inputs: original email/nullable label, with ID omitted.
- Mutators: original raw email, zero attempts, empty nickname and enum input, with ID omitted.
- Observers: original raw name and omitted ID, plus the retained returned-model oracle.

The mutator and observer checks now detect changes concealed by idempotent
normalization. Binary ownership, audit disclosure, timestamps, scopes, error
identity/redaction and relocated legacy oracles retain their assertions.

Review acceptance: PASS build/vet/race for `database/orm` and consumer
`hookqueries`, `inputqueries`, `linkqueries`, `mutatorqueries`, `observerqueries`;
PASS all 82 selected compiler cases in each fixture; PASS footprint, catalogue,
docs, gofmt and diff check. Initial docs invocation lacked the exported Go environment
and hit a sandbox default-cache denial; the r7 rerun passed. All Go build/test runs
used r7 with GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.

Additional docs notes: publish `RestoreEach(ctx, db, Users.Query(), Users.Soft, limit)`
and `ForceDeleteEach(ctx, db, Users.Query().OnlyTrashed(), Users.Soft, limit)` alongside
UpdateEach/DeleteEach. Explain explicit force visibility, restore-only-trashed behavior,
per-model callbacks, atomic bounds and reconciliation candidates. Linkqueries' batch
cutover is complete; lane A streaming and lane C shared-model upserts remain pending.
