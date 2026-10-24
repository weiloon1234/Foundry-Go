# Stage 9 — lane C wave 1

Base: `6d80111`. No commit. All Go commands use the r6 cache, `GOMAXPROCS=4`,
`GOFLAGS=-p=4`, and `GOWORK=off`.

## Publish-ready packages

| Package | Exports and cutover |
| --- | --- |
| `consumer/reporting` | Existing `MemberRow`/`MemberRows`, `OrderRow`/`OrderRows`, `MemberTotal`/`MemberTotals`, `Members`, `Orders`, `Totals`, and `ExportMembers` retain their identities. Added `OperatorModelColumns`, `OperatorModels` and `OperatorIdentity`; auth lookup, proofs, queued actor resolution and permission-change tests use ORM declarations. Existing local member/order declarations and relations remain the shared owners. |
| `consumer/intervalqueries` | Added `Samples`, `Plans`, `Labels`, `Links`, `PlanLinks`, `PlanValues`, `Summaries` and their column structs. Ported reads/writes, interval primary keys (explicit interval codec), pivot relations, aggregate slots, arithmetic, nullable/selected/window expressions and cursors. |
| `consumer/transactionqueries` | Added `Records`, `Summaries`, `TotalRows`, `Pairs` and their column structs. `Claimed`, `Joined` and `CountClaimed` retain their signatures. Ported CTEs, transaction joins, scalar membership, correlations, LATERAL, nullable projections, locks, savepoints and cancellation. |

Active consumer callers are migrated. Final cleanup/retirement remains at the
lead's generation barrier: model/projection directives, generated files and
ownership manifests were retained. Independent legacy SQL/identity goldens remain.
No framework APIs, method snapshots, dependencies, Makefile or generated output changed.

## Compiler cases

Ported **64 existing consumer cases**: `query_icontains_value`, `query_icontains_nullable`, all 53 `transaction_*` cases and these nine
interval cases: `interval_clock_instant`, `interval_find_number`, `interval_like`,
`interval_nullable`, `interval_projection_type`, `interval_result_phase`,
`interval_selected_row`, `interval_sum_decimal`, `interval_wrong_owner`.
Their existing goway equivalents and catalogue dispositions remain. Only these
owned catalogue records and their consumer diagnostic entries changed.

All **355 lane C cases pass in recorded segments** (354 initial passes; the
unlifted-correlation case was rerun after matching the new `expr.Predicate`
diagnostic; the two reporting text cases were rerun after their declaration cutover). Existing 14 datatable cases and the importer callback-owner case pass.

## Acceptance

| Check | Result |
| --- | --- |
| `go build ./reporting ./intervalqueries ./transactionqueries` (consumer module) | PASS |
| `go vet ./reporting ./intervalqueries ./transactionqueries` (consumer module) | PASS, including PostgreSQL test sources |
| `go test -race -v` for those three packages | PASS in segments; PostgreSQL opt-in variables unset, live integration cases skipped |
| Consumer compiler harness, exact lane C ownership selection | PASS, 355 cases in segments |
| `make datatable-contracts` | PASS: manifest/OpenAPI, full/surface TypeScript, shared modules and React/Vue adapters byte-identical; reporting route/DTO coverage passes |
| `python3 -B tools/goway_catalogue.py` | PASS: 1,026 cases; 534 ported, 48 pending, 444 unaffected |
| `make docs-check`, gofmt, `git diff --check` | PASS |

Corrections found during acceptance: use `Table.Insert`, wrap conflict targets
with `orm.ConflictKey`, use `expr.Not`, supply explicit interval codecs for natural
primary keys, and match the unlifted-scope predicate diagnostic. The tests retained
their behavior/assertions. The old/new SQL goldens for reporting, interval model /
relation / aggregate declarations and transaction model / projection declarations
pass. Live PostgreSQL, full integration, TypeScript execution and editor checks
remain with the lead.

No footprint ceilings or probes changed. This is consumer declaration/caller work;
framework generic families and public method counts are unchanged. The four
consumer footprint probes remain lane D's responsibility at integration; no new
consumer footprint measurement is claimed here.

## Blocked packages

Both independent C2/C3 packages (`intervalqueries`, `transactionqueries`) are done.
The remaining 26 packages were not edited:

| Prerequisites | Packages |
| --- | --- |
| A1 shared models; explicit lead release required | `reports` |
| `queryfixture` | `framequeries`, `jsonqueries`, `temporalqueries` |
| `models` and `queryfixture` | `chunkqueries`, `correlations/nullable`, `keyqueries`, `lockqueries`, `scalarqueries`, `windowqueries` |
| `models`, `reports` and `queryfixture` | `advancedqueries`, `comparisonqueries`, `correlations`, `ctes`, `distinctqueries`, `expressionqueries`, `joinqueries`, `joinqueries/inner`, `joinqueries/natural`, `joinqueries/outer`, `lateralqueries`, `projectionqueries`, `recordqueries`, `recursivequeries`, `setqueries` |
| The preceding shared foundations plus `windowqueries` | `filterqueries` |

## Requests to other lanes

- **D:** reporting DTOs, rows schemas, tables and route/job signatures are ready.
  Editor anchors are the handwritten `reporting/orm.go`, `reporting/rows.go`,
  `intervalqueries/orm.go`, `transactionqueries/orm.go` and transaction functions
  in `transactionqueries/models.go`. No editor or clientcontract files were edited.
- **B:** `interval_draft_text` remains your core-write case. Its ORM replacement is
  `intervalqueries.Samples.Period.Set("1 day")`; retain the `as temporal.Interval`
  diagnostic. It was not edited by lane C.
- **D/lead:** `importer_column_row` only checks incompatible ordinary DTO callback
  owners. It still passes without legacy model/query symbols. The catalogue's
  Framework-integrations area currently allows only Stage 7, whereas `unaffected`
  requires stage `—`. Its pending disposition was left unchanged for a catalogue
  classification decision; no duplicate case or tool change was introduced.

## Shared files touched

`blueprint/goway-data-layer/compilefail-mapping.tsv` (64 owned records), and
`tests/fixtures/consumer/types_test.go` (owned diagnostic entries only).
All other changes are in the three owned packages, their owned compiler case
programs, and this wave's report/evidence. Copied prerequisites: none.

## Docs notes

For the lead's changelog: reporting's operator authority reloads and export actor
references now use handwritten ORM identity declarations. Independent interval and
transaction-composition consumers use ORM/expr/rows, preserving their public result
identities and integration assertions. Generated SQL helpers coexist until the
Stage 9 cleanup barrier. Reporting's DTO/manifest/OpenAPI/TypeScript contract is
unchanged. Package READMEs document the new declaration entry points.
