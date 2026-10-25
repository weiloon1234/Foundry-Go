# Stage 9 lane C: relation ordering and cursor validation

Implemented on 68885959 above the accepted consumer-terminals and derived-gaps deltas. No commits, git writes, generated-output changes or module changes.

## API and ownership

- `expr.RelationOrder(relation, order)` adapts a computed row order to the actual relation target. `expr.RelationPivotOrder(relation, order)` does the same for the concrete pivot. Both reuse the shared relation-construction source/target validator and check expression references against the actual declaration. Direction, NULL placement, bound values and mixed target/pivot ordering are retained.
- `rows.CompileCursor` and `rows.PreflightCursor` inspect record cursors; `rows.CompileValueCursor` and `rows.PreflightValueCursor` cover scalar cursors. They delegate to `ormcore.CompileResultCursor`, preserving the completed-result boundary and canonical ordering without a page predicate or lookahead limit. Preflight uses the executing context and honors cancellation.
- The two internal-facing metadata bridges validate their capability before reading inputs. No capability reaches an application-implemented interface. Zero inferred capabilities, promoted relation-target markers, foreign declarations and wrong phases fail closed.

Eight public function additions, zero removals in this slice: `expr.RelationOrder`, `expr.RelationPivotOrder`, `orm.FoundryRelationOrder`, `orm.FoundryRelationPivotOrder`, `rows.CompileCursor`, `rows.PreflightCursor`, `rows.CompileValueCursor`, `rows.PreflightValueCursor`. No types or Query/Table/column methods were added, and no convertibility exception was added. The cumulative worktree snapshot includes the earlier accepted nineteen additions.

## Consumers and regressions

`scalarqueries` and `keyqueries` are migrated with no remaining framework blockers. Runtime consumer code uses ORM declarations; generated APIs remain only as SQL parity oracles. The two existing PostgreSQL relation-order tests retain their result assertions. The computed cursor-key negative still asserts compilation failure, with preflight rejection added; it does not paginate to obtain its error.

The prior untracked `scalarqueries/relation_order_pending_test.go` and `keyqueries/cursor_compile_pending_test.go` are replaced by `relation_order_postgres_test.go` and `cursor_compile_test.go`. Drop the pending files if applying this slice incrementally over the previously saved patch.

SQL/binding goldens cover direct relation, through-target, pivot and interleaved orders, plus grouped record and nullable scalar cursors. Runtime negatives cover foreign same-type declarations, forged relation targets, zero seals/orders/relations, cursor identity/order errors, stale scope context and cancellation. Five new external compile-fail cases cover relation target, selected order phase, pivot target, transaction input and promoted cursor input. Existing catalogue dispositions are unchanged.

## Footprint

Go 1.27.1 darwin/arm64, archive T/t symbol counts using the existing harness. New ceilings are `ceil(measured × 1.15)`; existing ceilings are unchanged.

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| relation_expression_order (new) | 4,232 | 4,867 |
| cursor_terminal (new) | 1,793 | 2,062 |
| q_with | 1,211 | 1,211 |
| aggregate_families | 4,366 | 4,369 |

The relation probe includes direct, through-target and pivot ordering across their model families. The cursor probe includes both complete-model and nullable-scalar compile/preflight paths. Neither adds methods to existing generic types.

## Acceptance

Targeted framework/consumer build, vet, tests and races pass, including the public-surface and convertibility guards. The relation fixture tests/races, 373 owned compiler cases, five new compile-fail cases and all footprint budgets pass. PostgreSQL source compilation, catalogue, docs-check, gofmt and `git diff --check` complete the gate; exact command results and hashes are in the accompanying JSON. All commands use r11 with `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. Live PostgreSQL remains the lead's integration gate.

## Shared files touched in this slice

`database/orm/method_surface_test.go`, `database/orm/testdata/public-methods.txt`, `database/orm/expr/README.md`, `database/orm/rows/README.md`, `tests/fixtures/goway/compiler_cases_test.go` and `tests/fixtures/goway/footprint_test.go`. Runtime changes are isolated in three new files: `database/orm/relation_expression_order.go`, `database/orm/expr/relation_order.go`, and `database/orm/rows/cursor_terminal.go`. No edits to another lane's runtime files.

## Docs notes

For the lead's changelog: computed target and pivot relation ordering now accepts validated expression adapters. Completed-result cursors expose compile/preflight terminals that reject invalid keys before I/O while preserving their SQL boundary. Scalar and key consumers now use ORM throughout their runtime paths.
