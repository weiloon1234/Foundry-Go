# Stage 10 lane B documentation

Base: `54508185`. Includes the lead-approved bounded-adjustment runtime correction after review last-30. No generator, editor, contract or footprint-budget files changed. The work leaves version control to the lead.

## Guides and compiled examples

Rewrote these 17 guides for shipped ORM APIs:

- `model-writes`, `model-batch-writes`, `model-changes`, `model-hooks`, `model-retrieval`;
- `model-global-scopes`, `model-timestamps`, `model-soft-deletes`;
- `model-mutators`, `model-accessors`, `model-upserts`, `model-lookup-writes`;
- `model-insert-from-query`, `model-source-writes`, `model-pruning`, `query-plans`, `audit`.

Added [typed declaration roles](../guides/model-declarations.md) covering managed timestamps, primary/input roles, soft tokens and source mapping restrictions. Corrected the migration guide's obsolete same-type `orm.Mutate` option advice: input constructors take a typed transform argument.

The 18 feature guides contain 38 Go blocks from 36 distinct compiled regions. Twenty-three regions are new; the others reuse the accepted migration package. The new archive declaration is checked alongside the existing fixture declarations. Every feature-guide Go block must match its compiled region exactly; new regions must appear in a guide. The migration guide retains its existing coverage and frozen oracle assertion.

Sources: [example functions](../../tests/fixtures/goway/migration/lifecycle_test.go) and [snippet/declaration checks](../../tests/fixtures/goway/migration/guide_test.go). Executor-taking examples are compiled, not executed against a database. The bounded-adjustment follow-up adds PostgreSQL behavioral assertions in `tests/fixtures/goway/pgsmoke/adjustment_test.go`; these sources are compiled, with live execution reserved for the lead.

## Acceptance

Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r14 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

| Command | Result |
| --- | --- |
| `go test ./migration` in `tests/fixtures/goway` | Pass; final run includes archive declaration and all snippet checks. |
| `make docs-check` | Pass. |
| `gofmt -w` on the two changed example test files | Complete. |
| `git diff --check` | Pass. |
| `make agent-smoke` | Lane D owns the editor suite; not run by lane B. |

No Go runtime API or public method snapshot additions. No persisted or wire-contract changes. No probe ceilings changed. Follow-up runtime verification and footprint measurements are recorded below.

## Docs notes

Suggested lead-owned status text: “Stage 10 lane B guides now describe typed declarations, mutation inputs, hooks/observers, retrieval, scopes, stored changes/audit, soft deletes/timestamps, conflicts, source/batch writes, pruning and explain. Exact compiled snippets cover each rewritten guide. Editor completion/hover/definition coverage remains lane D's integration responsibility.”

Suggested CHANGELOG note for the lead: “Bounded per-model adjustments now distinguish a missing/hidden key (`database.NotFound`) from a visible row rejected by the bound (`orm.ErrBoundExceeded`), with identical hooked and unhooked behavior. Empty guarded results add one scoped key-only locking read; successful writes and unknown-commit handling are unchanged.”

Preserved existing inbound anchors for transaction outcomes, conflict calculations, input types and field notices. No blueprint, master status, CHANGELOG, AGENTS.md or generated output was edited.

Stale content outside this focused guide set, for integration coordination:

| Files/sections | Remaining content | Owner/follow-up |
| --- | --- | --- |
| `model-generation.md`, `agent-language-tooling.md`, `developer-tooling-and-testing.md` | Model generation and managed per-field notice instructions. | Lane D editor/scaffold docs; coordinate generator removal with lane A. |
| `model-relations.md`, `model-relation-writes.md`, `model-relationship-filters.md` | Generated query, field and relation helpers. | Lane A relation ownership. |
| `orm-migration.md`, relation-loading section | Says `LoadFrom`/`LoadMissingFrom` are absent, but both are shipped in `database/orm/relation_tree.go`. Needs the relation example updated too. | Lane A relation ownership. |
| `model-queries.md`, expression/aggregate/rows guides | Generated query and SQL examples. | Lane C's parallel documentation slice. |
| `database-runtime.md`, observer paragraph | Generated observer registration helpers. | Lead coordinates the cross-cutting runtime guide with lane A application docs. |
| `events.md`, lifecycle dispatch; `outbox.md`, model snapshot/field sections | `PlainObserver`/`PlainChanges` and generated field wording. | Lead assigns these cross-cutting event/queue sections. |
| `binary-models.md`; `encryption.md`, encrypted model section | Generated binary operations and a generated encrypted-model write example. | Lead assigns these codec/security guide sections. |
| `database/orm/README.md` | Historical statements defer hooks, mutators and relation writes despite their shipped implementation. | Lead coordinates shared package README with feature lanes. |

The migration guide's PostgreSQL `Config.RegisterTypes` example remains blocked: H1's field is still absent from this branch's `database/postgres/config.go`. Its documented accepted contract remains unchanged.

## Review last-30: legacy adjustment result and outcome inspection

The legacy API does **not** expose per-model numeric adjustments. `Increment` and `Decrement` delegate to set-based `query.AdjustAll` (`internal/generate/emit_model_set_writes.go:28`, `database/query/set_write.go:62`). That terminal returns `RowsAffected` (`database/query/set_write.go:106`): both a rejected explicit guard and a missing key return `(0, nil)`. Observed models require `WithoutModelHooks`; their callbacks never run on this operation.

The nearest legacy per-model operation is `Patch` with explicit guard predicates and a literal assignment. It uniformly returns `database.NotFound` when no row matches. The hooked path fails in its locking read (`database/query/model_hooks.go:109`); the unhooked path fails on empty `RETURNING`. Both use `database/query/execute_mutation.go:246`. Neither identifies why a predicate matched no row.

The lead approved a new precise per-model contract: missing or hidden key → `database.NotFound`; bound rejection for a visible key → `ErrBoundExceeded`. Both the plain UPDATE and lifecycle locking-read paths consume and close their original result, then classify a clean empty result through `writePlan.returningAdjustment`. One key-only SELECT FOR UPDATE removes exactly the assignment's guard, preserving all other predicates, scope context, soft-delete visibility and lock behavior in the same transaction. No model callbacks or hydration occur for that diagnostic read. Success does not add statements. Set writes and unknown-commit handling are unchanged.

The guard's position is retained explicitly; the one-line change to lane A's `relation_pivot_mutation.go` offsets that position when composing a pivot plan, preserving the concrete key appended later. No public methods, capabilities or declaration changes were needed.

Regressions cover plain, local-hook and observer paths: visible bound rejection, missing key, tenant-hidden row, soft-deleted row, caller-predicate-hidden row, success, row-close failure, diagnostic query failure and cancellation. They inspect SQL predicates, key-only selection, context bindings, locks, row closure, transaction count, statement count and callback suppression. PostgreSQL sources additionally assert persisted balances and statement counts on plain and hooked paths. The legacy characterization remains to establish why this is an approved new behavior rather than a claim of old per-model adjustment parity.

The write guide restores the 256-node/64-wrapping-level inspection limits from `internal/errorgraph/walk.go:24` and `:29`. It explicitly warns that candidates can lack readable `database.Error` metadata and must never imply rollback, success or permission to retry.

## Bounded-adjustment follow-up acceptance

All commands use the r14/limited-CPU environment above.

| Command | Result |
| --- | --- |
| `go test -race ./database/orm ./internal/ormcore ./internal/querycore` | Pass; includes the new plain/hook/observer cases and existing transaction outcome tests. |
| Go-way `go test -race -count=1 -timeout=20m` in package batches of six, excluding `pgsmoke` | Pass as a segmented run, including compiler negatives, goldens, migration examples and fixture models. |
| `go test -race -count=1 ./cmd/freeze-oracles -run '^TestReplayGroupsRequirePostgresOnlyInFullCapture$'` | Pass after the census correction below; remaining batches then completed. |
| Go-way `go test -count=1 -v -run '^TestFootprintBudgets$' .` | Pass; this check is excluded from race builds and was run separately. |
| Go-way `go test -run '^$' ./pgsmoke` | Pass; PostgreSQL sources compiled, no live database execution. |
| Go-way `go vet ./...` | Pass. |
| `go vet ./database/orm ./internal/ormcore ./internal/querycore` | Pass. |
| `make docs-check` | Pass. |
| `python3 tools/goway_catalogue.py` | Pass; 587 ported, 0 pending, 445 unaffected. |
| gofmt on changed Go files; `git diff --check` | Complete/pass. |

The first fixture batch passed its compiler and golden tests but the freeze harness rejected a stale source census. Reviewed and refreshed exactly three entries in `tests/fixtures/goway/testdata/oracles/comparisons.json`: the changed `migration/guide_test.go`, added `migration/lifecycle_test.go`, and added `pgsmoke/adjustment_test.go`. The existing paired-test list is unchanged; the two new files contain no paired legacy comparisons. Frozen records, publication state and generated legacy models are unchanged. The failed harness test passed after this metadata correction; already-passing packages were not rerun.

Earlier corrections: one unused import was removed before the original characterization test passed. A docs-check attempt omitted the cache environment and could not access the default cache; the final r14 run passed.

| Footprint probe | Functions | Existing ceiling |
| --- | ---: | ---: |
| `q_with` | 1,211 | 1,211 |
| `aggregate_families` | 4,365 | 4,369 |
| `terminal_adjustment` | 1,381 | 1,590 |

Every probe passed; no ceiling or probe definition changed. No surface additions or catalogue-row changes. No persisted schema or wire changes.

Shared files touched by the follow-up: `database/orm/README.md`, `database/orm/orm_test.go` (optional driver affected count), `database/orm/relation_pivot_mutation.go` (guard-index offset), and the three owned census entries above. Existing slice shared edits remain `docs/guides/orm-migration.md` and `tests/fixtures/goway/migration/guide_test.go`. Runtime changes are confined to `database/orm`; `ormcore`, `querycore` and the legacy query engine were verified/reused without edits.
