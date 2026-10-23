# Stage 6c2: CTEs, recursive CTEs and set operations

## Inventory and slice plan

The old API has complete-record CTEs, materialization hints, dependency ordering,
recursive UNION/UNION ALL definitions and a non-executable recursive self. Its
record and scalar sets cover UNION, INTERSECT and EXCEPT with ALL variants,
separate input/output scopes, exact result types and preserved operand windows.
Transaction CTEs retain locks and require transaction-only consumers. The shared
querycore compiler already owns all of these SQL rules.

The accepted three-slice plan remains:

1. Complete record/projection adapters and transaction foundations — accepted 6c1.
2. Ordinary/transaction CTEs, recursive CTEs, record/value set operations and the
   deferred lateral/CTE rejection — this delivery.
3. Result cursors, remaining read/page/scalar transaction/projection-lock terminals
   and row-value cases; coordinate transaction joins with 6b4.

This slice ports 25 Stage 6c rows plus Stage 6b's `lateral_record_cte`. Stage 6c now
has 47 of 73 rows ported; 26 remain. Five additional cases cover structural phase
and transaction/source escapes. No missing-symbol future API substitutes for a
ported case.

## API

`CTE` accepts an ordinary complete `RecordSource`; `TransactionCTE` accepts only a
`TransactionRecordSource`. Their `CommonTable` and `TransactionCommonTable`
results expose `Materialized` and `NotMaterialized` and retain the existing
ordinary/transaction alias families. Definitions are immutable, reused references
emit once, dependencies precede consumers, and conflicting names fail closed.
Transaction CTEs retain locks and wait policy inside their defining SELECT.

`RecursiveCTE` and `RecursiveAllCTE` invoke one synchronous construction callback
with `RecursiveSelf[R]`. Self can be aliased but has no execution/mutation methods.
The existing compiler enforces exactly one self reference in its owning step,
rejects independent nested capture or escape, and enforces PostgreSQL's recursive
restrictions. Anchor/step record layouts and executor kinds must agree. Recursive
NotMaterialized fails. Application data must ensure termination; execution should
have cancellation/deadlines, and an outer LIMIT is not a general recursion bound.

`Union`, `UnionAll`, `Intersect`, `IntersectAll`, `Except`, `ExceptAll` return
`SetQuery[R]`. Its `Scope` has the distinct `Set[R]` type. `Where`, `OrderBy`,
`Limit`, `Offset`, `All` operate on the combined output; each input's clauses stay
inside its operand. The result supports complete record selection, projection,
aliasing and CTE composition, preserving the decoder and retrieval owner.

`Values` adapts an existing typed `Scalar` into `ValueRecord`. `ValueSource[V]`
retains the exact scalar type, codec and nullability through the six corresponding
`*Values` set functions. `ValueSetQuery[V]` exposes `Value`, `Where`, `OrderBy`,
`Limit`, `Offset`, `All`. SelectScalar can adapt its output back into shared
scalar/IN/EXISTS compilation. A value set also supplies its complete one-column
record for CTE composition. Ordinary record sets are not scalar subqueries, and
transaction inputs cannot enter ordinary sets.

All new public generics contain thin/nongeneric state, have direct methods and
use existing validated expression capabilities. No capability enters application
callbacks. No Query/Table method changed. A single metadata getter carries each
source's decoder, AST and executor kind; separate promoted markers cannot replace
these validated data. The existing structural join-boundary checks remain intact.

## Shared implementation

`internal/ormcore/record_composition.go` retains declaration checks and constructs
existing CteNode, RecursiveReference, RecursiveCTE and SetNode structures. It uses
the accepted record reader and alias adapter. `querycore.CompatibleRecords` now
owns ordered record-layout validation; the old generic helper and the new adapter
both delegate to it. CTE, recursive, set, relation-EXISTS and subquery SQL compilers
are reused without duplication.

## Catalogue and regressions

The evidence JSON lists all 26 ported cases and five additions. The ports comprise
12 CTE/recursive cases, nine set cases, four transaction CTE/record cases, and the
previously deferred lateral-record CTE rejection. The registry now has 412 cases.

Runtime regressions cover definition reuse, dependency order, immutable hints,
conflicting/invalid names, zero and nil inputs, escaped/missing/repeated recursive
self references, nested capture, record layout mismatch, executor-kind mismatch,
input/output windows, scalar NULL decoding, and retained deferred checks from
both operands and recursive steps.

Nineteen old/new SQL, binding and result goldens cover all six operators on both
records and scalars, ordinary/materialized/dependent CTEs, both recursive forms,
transaction locks and projected CTEs. PostgreSQL sources cover recursive traversal,
set membership/multiplicity, scalar deduplication and two-transaction SKIP LOCKED
behavior. PostgreSQL execution remains with the lead.

## Footprint and public surface

The public snapshot adds 65 lines and removes none. Exact signatures are in the
JSON. Every addition is accounted for here:

- Value/pointer type entries for CommonTable, TransactionCommonTable,
  RecursiveSelf, Set, SetQuery, ValueRecord, ValueSetQuery: 14 lines.
- ValueSource and its sealed valueData signature: two lines.
- Materialized/NotMaterialized on both CTE types, in value/pointer method sets:
  eight lines.
- Scope/Where/OrderBy/Limit/Offset/All on SetQuery, and
  Value/Where/OrderBy/Limit/Offset/All on ValueSetQuery, in both method sets:
  24 lines.
- CTE, TransactionCTE, RecursiveCTE, RecursiveAllCTE, Values, Union, UnionAll,
  Intersect, IntersectAll, Except, ExceptAll, UnionValues, UnionAllValues,
  IntersectValues, IntersectAllValues, ExceptValues, ExceptAllValues: 17 lines.

Native Go 1.27.1 darwin/arm64, warm requested shared cache, using the existing
archive T/t-symbol measurement method:

| Probe | Functions | Ceiling |
| --- | ---: | ---: |
| New cte_recursive | 1,076 | 1,238 |
| New record_sets | 1,288 | 1,482 |
| New value_sets | 2,230 | 2,565 |
| Existing q_with | 1,209 | 1,211 |
| Existing aggregate_families | 4,362 | 4,369 |

New ceilings are ceil(measured × 1.15). Both sensitive counts match accepted 6c1
exactly. All 62 prior budget entries remain unchanged; there are now 65 probes.
The new probes exercise recursive/transaction CTEs, model/projected record sets
and required/nullable scalar sets, including actual All paths. No runtime
performance claim is made.

All 65 budgets pass.

## Shared files

Incremental existing-file changes from accepted 6c1:

- `database/query/set.go`: delegates record-layout validation and the set-output
  alias to the shared core.
- `internal/querycore/set.go`: owns the shared set-output alias.
- `database/orm/rows/record.go`: recognizes private scalar-record metadata for
  complete value-set CTE hydration.
- `database/orm/method_surface_test.go` and `testdata/public-methods.txt`: additive
  signature/surface coverage.
- `database/orm/rows/README.md`: links the dedicated composition guide.
- `tests/fixtures/goway/compiler_cases_test.go` and `footprint_test.go`: new cases
  and probes; existing budget entries are unchanged.
- `blueprint/goway-data-layer/compilefail-mapping.tsv`: only the 26 assigned rows.

New shared owners: `internal/ormcore/record_composition.go` and
`internal/querycore/record_layout.go`. Other additions are dedicated rows adapters,
regressions, consumer goldens, pgsmoke, probes, compiler cases, guide and evidence.
No lead-owned document, lifecycle implementation or aggregate implementation changed.
The incremental patch is based on the accepted 6c1 worktree, including its untracked
files, rather than only the original branch HEAD.

## Review correction

The second review identified a duplicated set-output alias. `querycore.SetAlias`
now owns `foundry_set`; both old query and new rows/ormcore adapters reference it.
This changes no SQL spelling, public method surface, catalogue row or budget.
The shared set compiler logic is unchanged.

## Acceptance

The final review-fix rerun uses `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2`
with `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off` for all Go commands.
Fixture execution explicitly clears both PostgreSQL environment controls.
No shared cache is deleted, no dependency is added, and no commit is made.
Final command statuses, source/log hashes and corrections are in the evidence JSON.

All requested acceptance commands passed after the SSOT correction.
Logs are retained in `/private/tmp/goway-6c2-ssot-logs`.

| Command | Result |
| --- | --- |
| `go build ./...` | Pass |
| `go vet ./database/... ./internal/...` | Pass |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | Pass |
| Fixture: `go vet ./...` | Pass, including pgsmoke sources |
| Fixture: `go test ./...` | Pass, including goldens, all 65 budgets and 412 compiler cases |
| `python3 tools/goway_catalogue.py` | Pass |
| `sh tools/format-go.sh check /usr/local/go/bin/gofmt` | Pass; repository-wide gofmt clean |
| `git diff --check` | Pass |
| `make docs-check` | Pass |

The additional docs check initially used the default Go cache and was denied by
the sandbox. Rerunning it with the requested environment passed; both logs remain
in the evidence.

All five reported footprint counts were remeasured and are unchanged. The fix
adds no public surface or catalogue rows. Its five source files are
`internal/querycore/set.go`, `internal/ormcore/record_composition.go`,
`database/query/set.go`, `database/orm/rows/set.go` and
`database/orm/rows/value_set.go`. The source-only review correction is retained at
`/private/tmp/goway-6c2-ssot-fix.patch`; the full incremental 6c2 patch is refreshed
at `/private/tmp/goway-6c2.patch`.

Historical first-pass acceptance was segmented: one additive surface snapshot
and one compile-fail diagnostic matcher required focused reruns. Those logs and
results are preserved under `previous_acceptance` in the evidence JSON. They are
superseded by the successful full rerun above.

Source/preflight corrections also fixed a golden's brace formatting, a fixture
import collision with the package's existing rows helper, and unsupported range
spellings in new examples (plain key ranges became typed In; the wrong-scope
expression uses Eq). No contract, diagnostic intent or existing ceiling was
weakened. The root build's module stat-cache write warning was nonfatal. The long
race compilation remained active alongside other workspace builds; no process or
shared cache was interrupted.

## Docs notes

For the lead's changelog/status: Stage 6c2 adds complete-record ordinary and
transaction CTEs, recursive UNION/UNION ALL, and all six record/scalar set operators.
Definitions, scopes, exact value/nullability types, input windows and transaction
locks remain explicit. Twenty-five more Stage 6c rows are ported, plus the deferred
6b lateral/CTE case. Result cursors and remaining terminals await 6c3. The package
recipe is `database/orm/rows/composition.md`. Shared lead documents remain untouched.

## Questions for the lead

No unresolved API question blocks this slice. Run the pgsmoke sources at the next
database checkpoint and coordinate transaction join/nullable-record cases with
6b4. Slice 3 remains for the next turn.
