# Stage 9 review 20: retained diagnostic values

Compiled statements (ORM, rows and legacy), captured assignment inputs, numeric
bounds and EXPLAIN plans now retain sensitive state behind pointers to private
non-generic structs. No methods were added. Statement formatting now omits SQL
as well as bindings; raw SQL can itself contain literals. `SQL`, `Arguments`,
mutation input inspection, and the explicit plan accessors preserve their values
and ownership. Input type preflight reads the retained value without invoking a
clone callback. `projection_bridge.go` has only the isolated statement-constructor
change for this review.

Regressions cover `%v`, `%+v`, `%#v`, private and nested fields, pointers and
`fmt.Errorf`. EXPLAIN tests check decimal and hexadecimal byte sequences, prove
those sentinels detect an unsafe inline control, and check explicit JSON snapshot
ownership. Assignment tests preserve clone counts, type rejection before mutation
and binding results. Existing SQL, lifecycle and compiler tests remain applicable.

## Main-branch port map

Read-only inspection of `main` at
`10030ad4eaf6ecf6caf269dfb5e970b0816f366e` found these exact exposure owners:

| Finding | Main files |
| --- | --- |
| Compiled statement SQL and arguments | `database/query/compiler.go:12` (`Statement`, its accessors and formatters). Migrate its construction and internal direct field readers along with the owner. |
| Assignment input | `internal/querycore/mutation.go:19` (`AssignmentValue.Value` and `InputValue`); `database/query/mutation.go:32` / `:43` (`Assignment`, capture); `database/query/mutation_input.go:18` (distinct input capture); `database/query/mutation.go:55` (`Mutation`); `database/query/mutation_values.go:11` (`MutationValues`). |
| Assignment preflight consumers | `database/query/field_mutator.go:112`, `database/query/conflict_mutator.go:24`: unwrap for exact type checks without invoking clone callbacks. These owners are in query on main, not in querycore as on this branch. |
| EXPLAIN JSON and decoded document | `database/query/explain.go:53` (`Plan`); construction in `database/query/explain_decode.go:96`. Main has not moved these owners to querycore. |
| ORM statements / rows / numeric bounds | No main counterpart: main has no `database/orm` tree. Legacy adjustment values are captured in binding closures, not an inline `Bound` field. |

Review 21 extends this map (read-only inspection of the same main commit):

- `database/query/cursor_token.go:25` owns the inline cursor token and its constructors/accessor. Its direct readers are `database/query/cursor_boundary.go:42` and `database/query/cursor_validation.go:26`; update them with the holder. On this branch these contracts share `internal/querycore/cursor_contract.go`. Preserve cursor value equality across independent parses: the private, method-free state uses Go’s interned handle; a fresh pointer fails the existing cursor alias and HTTP codec equality regressions.
- `database/query/relation_morph.go:138` / `:144` captures `pivotAssignment.raw`; `database/query/relation_through.go:29` retains the fixed assignments. Consumers are `database/query/relation_write_keys.go:45` and `database/query/relation_pivot_sync.go:469`. Reuse the literal holder and unwrap only at assignment capture.
- `internal/querycore/query_core.go` and `internal/querycore/select.go` retain structural `LIMIT`/`OFFSET`. The lead accepted these as non-secret query metadata; no holder change is intended for them.

For the preceding accepted AST redaction set, main also retains predicate state
in `database/query/ast.go`, `database/query/having.go`,
`database/query/global_scope.go`, `database/query/join_condition.go` and
`database/query/value_expression.go`. The common literal owners to port are `internal/querycore/ast.go`
(`Comparison.Values`, `RowComparison.Values`),
`internal/querycore/parameter_expression.go` (`ParameterNode.Value`),
`internal/querycore/window_function.go` (`WindowNode.Fallback` / offset) and
`internal/querycore/window.go` (`FrameBound.Offset`). Their constructors and
compiler reads must migrate together; adding formatters to public wrappers alone
does not protect private fields.

## Acceptance

Current-source targeted gate uses `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r9`,
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | PASS |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | PASS |
| Fixture `go test -v . -run 'TestFootprintBudgets\|TestExtensionMatching\|TestZeroExtensionDescriptorExtraction'` | PASS |
| Fixture `go vet .` | PASS |
| `PYTHONDONTWRITEBYTECODE=1 python3 tools/goway_catalogue.py` | PASS (548 ported / 34 pending / 444 unaffected) |
| `gofmt -l` on the 31 Go sources; `git diff --check` | PASS |
| `make docs-check` | PASS |

All 129 footprint probes passed with **no growth** against review 19: 114 exact
matches and 15 reductions. `q_with` remains 1,211/1,211;
`aggregate_families` is 4,365/4,369 (previously 4,366). The largest reduction is
`input_column_kinds`, 5,321 to 5,279. Full before/after counts are in the evidence.
No methods, catalogue rows or footprint ceilings were added or changed.

No PostgreSQL was run. There are no declaration, generated contract or transport
changes in this fix, so generation, TypeScript and editor checks were not rerun.
The build emitted the existing sandbox module-cache stat-write warning and exited
successfully; all subsequent commands passed without that warning.

## Shared files touched in this review

`database/orm/query.go`, `database/orm/projection_bridge.go`, `database/orm/lookup.go`, `database/orm/adjustment.go`, `database/orm/write_draft.go`, `database/orm/diagnostic_retention_test.go`.

`database/query/compiler.go`, `database/query/core_metadata.go`, `database/query/read_lifecycle.go`, `database/query/execution.go`, `database/query/relation_aggregate_loading.go`, `database/query/cursor_token.go`, `database/query/execute_autocommit.go`, `database/query/prune_batch.go`, `database/query/relation_pivot_sync.go`, `database/query/read_result.go`, `database/query/set_write.go`, `database/query/source_write_execution.go`, `database/query/insert_select.go`, `database/query/conditional_expression_test.go`, `database/query/statement_shape_alloc_test.go`, `database/query/insert_select_test.go`, `database/query/source_write_test.go`, `database/query/statement_retention_test.go`.

`internal/querycore/retained_state.go`, `internal/querycore/retained_state_test.go`, `internal/querycore/mutation.go`, `internal/querycore/field_mutator.go`, `internal/querycore/conflict_mutator.go`, `internal/querycore/explain.go`, `internal/querycore/explain_decode.go`.

Guides: `docs/guides/model-queries.md`, `docs/guides/model-upserts.md`.
The evidence JSON includes source hashes; existing accepted lane edits are preserved.

## Docs notes

Diagnostics for statements and plans omit retained SQL, bindings and server plan
content, including through private wrappers. Applications explicitly requesting
`SQL()`, `Arguments()`, mutation inputs or plan snapshots receive their ordinary
inspection data. Lead-owned documentation was not edited. No commit was made.
