# Stage 8d2 — lane B behavior ports

Base: `e7b4d8a4`. Test-only changes; no public API deletion or engine change.

## Port ledger

Existing test names remain the anchors unless listed below. All count, value,
error identity, cancellation and ownership assertions remain executable.

| File / old anchor | Native operation / new anchor |
| --- | --- |
| `database/model_mutation_test.go` — five `TestModelMutation*` / `TestModelInsertOnPool*` tests | Handwritten `mutationTable`; `Table.Insert`, typed assignments and `orm.WriteError`. Retains autocommit/transaction distinction, malformed/multiple/missing rows, nested scopes, unknown and committed outcomes, candidate redaction and connection disposal. |
| `database/model_batch_write_test.go` — `TestPerModelBatchWritesRunWithoutPerRowSavepoints` | `orm.CreateEach`; 70 returned models, no per-row savepoints, one enclosing savepoint in a caller transaction. |
| `database/model_hooks_test.go` — five tests | `orm.WithHooks`, `Saving`/`Updated`, `orm.NewObserver`. `TestRegisteredObserversRequireMatchingFamilyAndRunThroughActualTransaction` replaces the old adapter test's applicable cases. Wrong-family registration replaces a stale handwritten adapter; real transaction ownership, factory panic/Goexit, veto, closed snapshots and commit candidates remain checked. Another pool's observer factory must not run on an observer-free wrapper. |
| `database/model_lock_test.go` — both tests | Native locked model queries and `Require`; `rows.ForUpdate(rows.Read(rows.SelectValue(...)))` for scalar locking. Caller transaction, ordering, nil/closed/canceled rejection and partial hydration assertions remain. |
| `database/model_upsert_test.go` — four tests | `orm.Upsert`, `orm.UpsertMany`, `ConflictKey`/`ConflictIncoming`, `Table.InsertMany`; typed optional/batch candidates and empty-write no-I/O remain. |
| `database/model_pagination_test.go` — both tests | `orm.PageRequest`, package `orm.CursorPaginate`; request/window validation before I/O and all-or-error page/count metadata remain. |
| `database/result_cursor_test.go` — `TestResultCursorOwnsRowsAndDiscardsFailure` | `rows.CursorFor(rows.ModelRecord(...))`, typed output key, `orm.CursorPage`; one lookahead query, token directions, malformed lookahead and row-close failure remain. |
| `database/retrieval_dispatch_test.go` — seven tests | Native retrieval hooks/observers, model/record/alias/CTE/recursive/set/cursor/transaction sources, DTO declarations and `rows.EachRead`. Closed-row callback I/O, once-per-batch factories, original executor, registry isolation, shared depth, no retrieval on writes/scalars/DTOs, first-input set lifecycle, keyset windows and released capacity remain. |
| `database/routing_test.go` — five tests | Native `routedQuery`; low-level raw-driver, startup, replica failure, health and shutdown assertions stay independent of query frontend changes. |
| `application/maintenance_schedule_test.go` — `TestMaintenanceSchedulePrunesConfiguredFrameworkStores` | Handwritten `maintenanceFailures` and `prune.ORMModel`. All scheduled-store persisted-count and retention assertions remain. |
| `testkit/factory/factory_test.go` — five existing tests, plus composition callers | Removes generated model/draft interfaces. The shared-core tests keep their inspectable domain draft and convert it to native assignments at the existing prepare seam. Adds `TestORMFactoryBuildFailureDoesNotStartBatch` through public `Define`, covering returned error, panic and Goexit without partial models or database work. |

Native hook-free `Table.InsertMany` on an exact pool uses autocommit; legacy
`InsertMany` opened a transaction. The existing batch rollback/lost-commit cases
now use the already present `transactionOnly` writer to test the same explicit
transaction boundary. Assertions were not changed to accept uncertain outcomes
as rollback or success. The single-insert autocommit tests remain separate.

`database/prune/prune_test.go` already uses native limits and tests the shared
registry; it needs no source change.

## Retained dependencies and legacy-only contracts

- `database/legacy_adapter_error_test.go` retains the two old error-returning
  adapter-factory cases as `TestLegacyObserverAdapterErrorRollsBackBeforeSQL` and
  `TestLegacyRetrievalAdapterErrorDiscardsResults`. Native factories cannot return
  errors and adapters are framework-installed. These cases remain live pending a
  lead decision for 8d3, rather than being weakened to a different callback stage.
- That file also retains `retrievalFixtureDefinition` for lane C's pending
  `database/record_iteration_test.go` legacy capture. Lane C owns removing that
  dependency after capturing the comparison.
- `attachments/collections_postgres_test.go` remains unchanged: its `Matching` and
  scalar query depend on lane D's legacy `extensiontest.Members`, `MemberQuery`
  and `MemberIDField`. A native owner must land first; this slice does not copy
  lane D's declaration. Its identity, ordering, localized empty/missing, typed
  scope and replacement assertions remain intact. The adjacent, unassigned
  `attachments/slot_postgres_test.go:129` also still calls legacy `With(...).Load`
  through the same owner fixture.
- Public legacy callers deliberately remain for 8d3: `database/prune/legacy.go`
  (`Model`), `testkit/factory/factory.go` (`New`), and attachment matching/slot
  bridges. Application maintenance production references only limit aliases;
  lane C owns that spelling migration in 8d1.

## Frozen evidence and footprint

No legacy/ORM comparison was removed in this lane, and no new oracle was captured.
Lane C owns the comparison captures. Existing frozen records, publication receipts,
source hashes, generated outputs and footprint ceilings are unchanged. Test-only
edits do not change probe reachability; no new footprint probe is needed.

## Verification

See `goway-stage8d2-b.json` for commands, results and source fingerprints. Private
PostgreSQL configuration was sourced without printing it. Sandbox network failures
are tracked separately from the successful permitted-network reruns.
