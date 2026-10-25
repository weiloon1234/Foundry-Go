# Stage 10 lane C: docs and compiled examples

Base: `54508185`. No runtime API or generated output changes.

## Delivered

Rewrote 27 guides for handwritten ORM declarations:

- Expressions: conditional expressions, computed keys, field computations,
  arithmetic/text, value comparisons, temporal, interval, geographic and JSON paths.
- Results: model reads and chunks, rows projections, aggregates/FILTER, windows and
  RANGE/named frames, derived/scalar records, joins, correlations and lateral joins,
  DISTINCT, CTEs/recursion, sets and transaction-required composition/correlation.
- Reporting: datatable source declarations and its DTO, export and import examples.

The datatable guide retains authority checks, filter/operator semantics, bounded
streaming ownership, repeatable-read transactions and wire-contract guidance.
JSON guidance now uses field-pointer declarations and typed map/index helpers.
Aggregate guidance names the exact, floating and interval families separately.
Read guidance distinguishes model, projection, scalar, record, set and cursor
terminals, including compilation/preflight without I/O.

The guides reuse 33 excerpts from handwritten, compiled consumer sources.
[The excerpt guard](../../tests/fixtures/goway/guideexamples/guides_test.go) requires
a source for each Go fence and checks its contents, allowing indentation changes.
The shared external-input fingerprint hook tracks changes outside the fixture
module under make; direct invocations use `-count=1`.
It rejects stale generated-query guidance while permitting ordinary DTO/enum
contracts. Each excerpt links its imports and enclosing declarations/test context.
No second example implementation or new dependency was added.

The expr and rows package READMEs no longer describe shipped text/temporal,
transaction, CTE and lateral support as future stages. The existing complete-model
join anchor remains available for blueprint links.

## Acceptance

All Go commands use r14, `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

- `make docs-check`: pass.
- Goway `go test -count=1 ./guideexamples`: pass, covering all 27 guides and 33 excerpts.
- Consumer package command recorded in the companion JSON: pass. It compiles all
  test sources in 25 packages and executes 11 selected SQL-only/contract tests.
  Packages reporting “no tests to run” were compile coverage, not runtime coverage.
- `gofmt` on the added Go test and `git diff --check`: pass.

No PostgreSQL tests ran. `internal/agent` and agent-smoke belong to lane D under the
Stage 10 brief. Public methods, footprint probes and ceilings are unchanged.

## Stale content outside this lane

These remain for their owning lane or lead; this change does not rewrite them:

| Location | Remaining guidance | Routing |
| --- | --- | --- |
| `docs/guides/model-generation.md:9` | Executable generated model queries/drafts; projection and scoped-field generation at lines 11–13 | Generator retirement / lead |
| `docs/guides/model-relations.md:7` | Generated relation sets, binding and factories | Relation owner / lead |
| `docs/guides/model-relationship-filters.md:9` | Generated fields and WhereHas/WhereDoesntHave examples | Relation owner / lead |
| `docs/guides/model-pagination.md:10` | Generated queries/projections and legacy cursor parsing | Lane D |
| `docs/guides/result-cursor-pagination.md:8` | Generated output fields and legacy query cursor APIs | Lane D |
| `docs/guides/database-codecs.md:43` | Generated drafts as omission example; generated model reads at line 65 | Codec/write documentation owner |
| `docs/guides/query-build-resources.md:54` | Generator-centric architecture and historical measurements | Lead: distinguish historical measurements from current architecture |

## Review 35 corrections and caution sweep

Re-read all 27 guides against the handwritten APIs and shared compiler/runtime,
and compared their prose with the pre-rewrite guides for still-valid cautions.
The 33 source excerpts are unchanged. No runtime or consumer source changed.

- Recursive CTE constructors accept ordinary record sources only. `TransactionOf`
  lifts a completed ordinary result; it does not admit locked recursive inputs.
- SELECT scopes are independent; CTE bodies and their consumer share compiler
  depth/node/parameter limits.
- Recursive streaming guidance again cancels a child query context before returning
  a callback error, links the retained cancellation example and leaves transaction
  recovery/completion with the caller.
- Restored recursive placement/materialization and traversal limits, explicit final
  ordering, pagination's outer-window rejection, physical SQL compatibility,
  nullable literal comparisons, stable custom key encoding, relation-key equality,
  callback/memory cautions and custom JSON serializer ownership. Clarified that an
  empty scalar subquery yields NULL.

The source sweep covered these guide groups:

| Guides | Implementation anchors |
| --- | --- |
| model-ctes, model-recursive-ctes | `rows/recursive_cte.go`, `rows/transaction_record.go`; `querycore/cte_compiler.go`, `select.go`, `select_walk.go`; `ormcore/source.go`; retained recursive cancellation consumer |
| model-projections, model-set-operations, model-subqueries, model-distinct | `rows/result_read.go`, `rows/pagination.go`; `querycore/report.go`, `set.go`, `distinct.go`, `value_expression.go` |
| model-joins, model-correlations, lateral-joins, transaction-query-composition, transaction-correlations, value-comparisons-and-joins | `rows` source/correlation/transaction declarations; `querycore/correlation_compiler.go`, `select.go`, `select_walk.go` |
| conditional-expressions, computed-query-keys, typed-field-computations, model-aggregates, model-aggregate-filters | `expr/conditional.go`, `aggregate_fields.go`, `aggregate_rows.go`, `filter.go`; `orm/relation_aggregate_loading.go`; `querycore/compiler.go`, `computed_expression.go`, `key_compiler.go` |
| model-queries, model-chunks | `orm/column.go`, `orm/chunk.go`; `ormcore/source.go` |
| model-windows, window-ranges-and-names, scalar-calculations, temporal-calculations, interval-queries, geographic-distance, json-models | Corresponding `expr` constructors and `querycore` compiler families, plus linked consumer declarations/tests |
| datatable | `datatable/declaration.go`, `pipeline.go`, `manager.go`, `export.go`; previous authority/filter/export/contract cautions remain intact |

Paths above are relative to `database/` for `orm`, `expr` and `rows`, and to
`internal/` for `querycore` and `ormcore`; `expr` and `rows` live below `orm`.
The follow-up reruns only `make docs-check`, the uncached excerpt guard and
`git diff --check`; prior consumer compilation is unchanged-source evidence.

## Docs notes

Suggested changelog text: advanced query and reporting guides now use handwritten
`orm`, `expr` and `rows` declarations. Examples are checked against compiled
consumer sources, including nullable JSON paths, aggregate families and read
terminals. DTO generation and public report contract identities remain unchanged.

Suggested blueprint status: lane C's Stage 10 documentation is implemented and
locally checked. Editor completion/hover/definition acceptance remains lane D's
separate delivery. Do not mark the whole stage complete from this lane's result.
