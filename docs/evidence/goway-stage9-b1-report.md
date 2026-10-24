# Stage 9 B1 — foundation publication

Base: `6d80111`. No commits or Git writes. Four foundations are publish-ready;
none is claimed fully migrated while retained generated callers and cleanup remain.
Continued B1 with `inputqueries` because its distinct-input negatives have no external
prerequisite. The approved plan places B2/B3 after foundation integration/rebase.

| Package | Published surface | Changed/new package files | State |
| --- | --- | ---: | --- |
| `linkqueries` | Seven typed tables, `MemberLinks`, `ORMHooks`, managed timestamp/input/soft-delete roles, retrieval callbacks | 6 | Publish-ready; remaining relation/source/lookup tests queued for B3 |
| `mutatorqueries` | `Members`, `MemberIdentity`; existing input/storage/enum/key/accessor types unchanged | 4 | Publish-ready; persisted-change PostgreSQL test migrated to ORM |
| `observerqueries` | `Records`, `Plains`, `Effects`, `ORMObserverHooks`; instance-owned registration selects one adapter family | 5 | Publish-ready; full read-metadata cutover blocked as below |
| `inputqueries` | `Members`, distinct EmailInput/StoredEmail and LabelInput/StoredLabel carriers, typed draft getters and stored changes | 4 | Publish-ready; legacy operation suite retained |

No framework surface additions. Consumer additions are the table namespace types,
values and two hook factories above, plus `Trace.ORMChanges` in link/observer fixtures
and `State.ORMCaptured` in inputqueries. No model fields/tags, enum/DTO declarations,
wire/schema artifacts, generated outputs, ownership manifests or module pins changed.
The lead still owns generation and cleanup at the barrier.

## Consumer compiler rows

**46 consumer programs ported**, retaining their existing catalogue IDs and guarantees:

- Mutators/input (8): `mutator_value`, `mutator_owner`, `mutator_nullable`,
  `mutation_input_draft`, `mutation_input_conflict`, `mutation_input_predicate`,
  `mutation_input_nullable`, `mutation_input_scope`.
- Observers (4): `generated_observer_owner`, `generated_observer_factory`,
  `observer_factory_owner`, `observer_constructor_owner`.
- Changes (4): `model_change_snapshots`, `model_change_draft`,
  `model_change_field_result`, `model_change_unknown_field`.
- Batches/lookups (10): all five `each_*` and five `lookup_*` cases listed in the
  accompanying evidence JSON.
- Insert-select/source writes (20): all eight `insert_select_*`, three
  `source_delete_*` and nine `source_update_*` cases listed in the evidence JSON.

These were already mapped to Go-way equivalents; this slice migrates their actual
consumer sources. Catalogue totals remain 534 ported, 48 pending, 444 unaffected.
B's three pending Stage 7 audit rows remain B2 work: `audit_field_value`,
`audit_history_id`, `audit_model_owner`. Other lanes' rows are unchanged.

## Acceptance

All Go/Make commands used `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r6`,
`GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`. Go commands below ran from
`tests/fixtures/consumer` unless indicated otherwise.

| Command | Result |
| --- | --- |
| `go build ./linkqueries ./mutatorqueries ./observerqueries ./inputqueries` | Pass |
| `go vet ./linkqueries ./mutatorqueries ./observerqueries ./inputqueries` | Pass; includes PostgreSQL test sources |
| `go test ./linkqueries ./mutatorqueries ./observerqueries ./inputqueries -skip '^TestPostgres'` | Pass |
| Same test command with `-race` | Pass |
| `go test . -run '^TestInvalidPublicContractsFailCompilation$/^(<46 case IDs>)$' -parallel=4 -timeout=10m` | 28 passed initially; 18 rejected correctly but needed updated generic diagnostic expectations; those 18 passed on focused rerun |
| `go test . -run '^TestFootprintBudgets$' -v` | Pass, four retained probes |
| Root `python3 tools/goway_catalogue.py` | Pass |
| Root `make docs-check` | Pass |
| gofmt on changed Go files; `git diff --check` | Pass |

PostgreSQL sources compile; PostgreSQL was **not executed**. New isolated PostgreSQL
coverage exercises pivot keys/local-before-observer order/clock/retrieval, distinct
inputs and stored changes, observer-only defaults, callback failure identity,
panic/Goexit/savepoint rollback and transactional effects. Existing assertions remain.

| Retained consumer probe | Committed measured functions | Current functions / ceiling |
| --- | ---: | ---: |
| model_type | 1,159 | 1,159 / 1,449 |
| model_isolated | 139 | 139 / 174 |
| model_draft | 225 | 225 / 282 |
| endpoint_plain | 1,065 | 1,065 / 1,332 |

No footprint budget or probe changed. Framework sources are unchanged; the tight
Go-way probes were not remeasured in this consumer-only slice. Exact current archive
sizes and source hashes are in `goway-stage9-b1.json`.

## Shared files and handoff

Only owned entries changed in `tests/fixtures/consumer/types_test.go` and
`blueprint/goway-data-layer/compilefail-mapping.tsv`. Package-local files: 19;
compile-fail files: 46; shared files: 2; evidence files: 2.

- **Lane D:** use `mutatorqueries.Members` and `MemberIdentity`. Model IDs, State,
  DisplayEmail, DisplayNickname, and explicit AccessID/AccessEmail/AccessNickname
  signatures are unchanged. `inputqueries.Members` also exposes distinct input/storage
  types without changing their identities.
- **Lane A:** link declarations and `MemberLinks` are available for relation negatives.
- **Lead/common lifecycle owner:** full observerqueries migration is blocked at
  `row_observers_postgres_test.go:33` (`checkReadObservers`). Its assertion inspects
  exactly two Record write factories, one Plain write factory and one Record retrieval
  factory, including after row closure/scope expiry, without constructing callbacks.
  ORM registrations store private `hookSet`/`retrievalSet`, so public
  `lifecycle.ObserverFactories[M, orm.Hooks[M]]` and the retrieval counterpart cannot
  inspect them. Need common-owner read-only registration metadata/count inspection,
  or an explicit decision on an equivalent assertion. Do not expose private adapters
  or capabilities. The legacy assertion remains intact; no boolean-only substitute.

## Docs notes

Publish foundations before dependent cutovers. Register only one lifecycle adapter
family per model/application. Preserve explicit accessors and stored predicate types;
all mutated fields use Input carriers even when input and storage share a Go type.
Keep generated APIs until dependent callers migrate and the lead runs generation.
B1 provides declarations, not permission to retire legacy outputs or golden oracles.
