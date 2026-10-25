# Stage 9 lane B — remaining read consumers

Packages taken: **retrievalqueries, cursorqueries, pagequeries**. Base 68885959.

## Completed

- Complete-model CTE streaming uses `rows.EachRead`; ORM declarations seed the
  records and every application registers only ORM retrieval observers. Removed
  the temporary legacy registration selector/adapter. The original bounded-batch,
  offset/limit, callback ordering, nested SQL, factory-count and early-stop/error
  identity loop is byte-identical.
- Grouped cursors and page counts use explicit report DTO schemas and rows
  projections, with exact nullable-decimal SUM/AVG codecs. Joined DTO DISTINCT ON
  keeps both winner orders and bidirectional traversal.
- Nullable DISTINCT scalar cursors/pages, value sets, window-selected scalars,
  nested CTE/input limits, set multiplicity and joined complete-model page counts
  use the integrated rows clauses and read terminals. Original assertions remain.

No new framework API, methods, capability bridges, declarations, schema/wire
contracts or catalogue rows. No generated outputs or shared files changed.
Footprint sources and ceilings are unchanged; no remeasurement for fixture edits.

## Still blocked — lane C

**Ordinary complete-record DISTINCT ON with input-scope ordering** is still absent:
`rows.SelectRecord` returns `rows.RecordRead[S,M]`, which supports ordering/windows
but has no DISTINCT ON method or package adapter. `rows.DistinctRecord` only means
plain DISTINCT; `rows.DistinctOn` accepts DTO/model `rows.Query`, not RecordRead.
The missing shape is an ordinary `RecordRead[S,M]` clause accepting
`...expr.GroupKey[S]` and preserving its input-scoped ordering and model metadata.

The exact remaining assertions are:

- `cursorqueries.TestPostgresLegacyCompleteRecordDistinctOnCursor`: select the
  joined buyer record, choose one winner per buyer with purchase ID descending,
  preserve full-model decoding, then traverse forward/backward with one SQL per page.
- `pagequeries.TestPostgresLegacyCompleteRecordDistinctOnPage`: same joined
  complete-model winner selection, with pagination total equal to two winners.

Both remain in `legacy_clauses_postgres_test.go`; their statements and checks are
retained. A DTO decoder or raw-SQL substitute would lose the complete-model
contract. Lane C owns the missing clause adapter; no lane A terminal gap remains.

## Roster audit

The accepted B write/lifecycle, upsert, D2 adapter, D3 service and clientcontracts
migrations have no other recorded consumer cutover blocker. Generated callback
factories/declarations remain intentionally for the lead's retirement barrier.
`linkqueries` retains its accepted legacy creation-interface oracle and SQL parity
oracle; these are compatibility coverage, not missing ORM callers. Its legacy setup
helpers are used only by that oracle. Clientcontracts is migrated; lane D's accepted `httppagination` implementation
still awaits lead integration and was not copied. Shared compatibility constants for chunk and
cursor limits remain at their existing owner, without a duplicated local constant.

## Acceptance and docs notes

Build, vet, all three package tests/races, ten consumer negatives and thirteen
Go-way negatives pass. The production build covers retrievalqueries; cursor/page
are test-only packages and compile through vet/tests. Catalogue is unchanged.

See [evidence](goway-stage9-b-read-completion.json) for exact commands and hashes.
Use r10 with GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off. PostgreSQL/Redis are disabled;
PostgreSQL sources compile and live assertions remain for the lead's gate.

For lead docs: complete-record retrieval streaming is migrated, and scalar/DTO
pagination clause blockers are closed. Cursor/page packages remain partial only
for the two lane C complete-record DISTINCT ON assertions above. No contracts,
TypeScript, manifests, method surface or footprint budget changes.
