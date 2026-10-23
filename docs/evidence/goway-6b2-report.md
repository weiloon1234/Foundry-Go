# Lane A, Stage 6b2

[Evidence and source hashes](goway-6b2.json). The incremental patch relative to
accepted 6b1 is `/private/tmp/goway-6b2.patch`, including all new files.

Built on accepted 6b1 in the current uncommitted worktree, whose base remains
`a5f30a6264aaaa24e671ddbc5f37df06e5f9e996`. No commits, PostgreSQL execution, new
dependencies or edits to lead-owned status/changelog files.

## Inventory and slice plan

The original 100 Stage 6b catalogue rows comprise 25 join/alias/scope cases,
11 independent subquery cases, 16 correlation cases, 15 lateral cases and
33 transaction variants. Slice 6b1 ported 15. This slice ports all 19 ordinary ON
and independent subquery cases assigned here; 66 Stage 6b rows remain.
The old API and existing rows inventory is retained in the
[6b1 report](goway-6b1-report.md).

1. 6b1 join kinds, side ownership and nullable scopes: accepted with no findings.
2. 6b2 ON comparisons and independent scalar/IN/EXISTS: this implementation.
3. 6b3 explicit correlation and cross/inner/left lateral: next slice.
4. 6b4 transaction-required variants: only after Stage 6c supplies its transaction
   record/projection adapters, as the lead decided.

## API and implementation

`OnEqual`, `OnNotEqual`, `OnLess`, `OnLessOrEqual`, `OnGreater`,
`OnGreaterOrEqual`, `OnLike`, `OnDistinctFrom`, `OnNotDistinctFrom` and explicit
nullable ordered/LIKE variants take row-phase `expr.RowValue` operands. Values
retain separate left/right scopes, exact concrete types and nullability. The
ordered constraint reuses `expr.OrderedScalar` over the existing expression
constraint. No constraint matrix or SQL compiler is copied.

`OnAnd`, `OnOr`, `JoinOn.Not`, `WhereLeft` and `WhereRight` retain both provenance
sets. The non-generic `ormcore.JoinCondition` owner snapshots and bounds children
and reference sets, preserves operand errors and validates each side at binding.
`rows.On` uses that same comparison owner. Right-side ON filters on a LEFT JOIN
preserve unmatched left rows, unlike WHERE predicates on the resulting join.

`From` and `FromRows` return thin `Input[S]` adapters over existing non-generic
source state. `SelectScalar(source, selected)` returns `Scalar[S,V]`, retaining
exactly one selected value and its concrete codec closures. `WhereScalar` adds
row predicates. Scalar descriptors do not execute independently. General
projected records cannot accidentally satisfy the single-column contract.
Existing `SelectValue` remains unchanged; no Query or Table methods were added.
Projected and scalar filtering share the non-generic `ormcore.WhereSource` owner.

`ScalarQuery` and `ScalarNullableQuery` return selected expressions; their row
counterparts return `expr.RowExpression`. Required scalar input widens by one
nullable layer, while the explicit nullable path retains one. Empty results
become NULL. Multiple rows retain PostgreSQL error 21000; no implicit LIMIT is
added. Inner aggregates remain inside their SELECT phase. Immediate inner/outer
source errors, including zero descriptors, remain invalid through subsequent
expression/parameter construction. Deferred declarations are checked when their
SELECT is compiled.

`InQuery` checks exact row/result types. `InNullableQuery` explicitly accepts
nullable inner values. `expr.Not` preserves SQL three-valued truth, including
NOT IN with NULL input rows. `ExistsQuery` retains inner selections, aggregate
semantics and windows, including LIMIT zero, while anchoring ownership to the
concrete outer declaration.

All SQL lowers to existing `querycore.Subquery`, `SubqueryPredicate` and
`ScalarSubquery` nodes. These use the same `SubquerySQL`, `SubqueryPredicate` and
SELECT compiler used by relation EXISTS in `relation_exists.go`. Independent
queries pass no correlation requirement. Declaration checks use the existing
`Subquery.RelationCheck` boundary, so validation stays lazy and runs before SQL.
Nested statements share depth, node, parameter and SQL-size bounds. No compiler
file was changed or duplicated.

Five concrete expression bridges validate issued expression-family capabilities.
No implementation constructs a zero seal or hands an issued capability to an
application-implementable interface. Zero-capability tests cover extraction and
adoption. The package recipe is `database/orm/rows/README.md`.

## Compiler, SQL and PostgreSQL sources

Nineteen old catalogue rows are ported, with only those TSV rows changed relative
to accepted 6b1. Six additional negatives cover non-scalar sources, selected IN
operands, non-executable descriptors, locked EXISTS inputs, wrong selection owners
and selected scalar results entering row operations. The fixture now contains
225 cases. Exact case names and grouped remaining rows are in the evidence JSON.

Old/new goldens cover every ON comparison variant, computed operands, AND/OR/NOT,
side filters, independent scalar/IN/EXISTS, nullable results, aggregate phases,
window/limit preservation and binding order. Unit sources cover NULL hydration,
foreign declarations/aliases, nil/zero inputs, immutable derivations, deferred
validation, invalid exemplars through parameters and shared nested SELECT bounds.

PostgreSQL sources cover nullable IN/NOT IN, empty/NULL scalar hydration,
multiple-row cardinality failure, scalar aggregate phase ownership, soft deletes,
EXISTS over aggregate LIMIT zero, and the observable ON-versus-WHERE distinction
on a left join. Sources compile and vet; the lead runs live PostgreSQL.


## Footprint and public API snapshot

Native Go 1.27.1, darwin/arm64, shared warm cache. These are T/t function-symbol
counts in consumer export archives, not runtime timings. The initial measurement
was confirmed on the final source; the existing fixture guard also passed all
47 probes and the model slope. All 45 pre-existing baselines/ceilings are unchanged.

| New probe | Functions | Ceiling, ceil(measured × 1.15) | Archive bytes |
| --- | ---: | ---: | ---: |
| `on_comparisons` | 1,454 | 1,673 | 6,546,024 |
| `subqueries` | 2,837 | 3,263 | 12,192,498 |

The public snapshot adds exactly 44 lines, removing none. Each addition is:

- Value/pointer entries for `rows.Input` and `rows.Scalar`, plus the shared
  `expr.OrderedScalar` constraint entry: five lines.
- `JoinOn.Not`, `JoinOn.WhereLeft` and `JoinOn.WhereRight` on value and pointer
  method sets: six lines.
- Sixteen ON function signatures: `rows.OnEqual`, `OnNotEqual`, `OnLess`,
  `OnLessNullable`, `OnLessOrEqual`, `OnLessOrEqualNullable`, `OnGreater`,
  `OnGreaterNullable`, `OnGreaterOrEqual`, `OnGreaterOrEqualNullable`, `OnLike`,
  `OnLikeNullable`, `OnDistinctFrom`, `OnNotDistinctFrom`, `OnAnd` and `OnOr`.
- Eleven source/subquery function signatures: `rows.From`, `FromRows`,
  `SelectScalar`, `WhereScalar`, `ExistsQuery`, `InQuery`, `InNullableQuery`,
  `ScalarQuery`, `ScalarNullableQuery`, `ScalarRowQuery` and
  `ScalarNullableRowQuery`.
- Six expression signatures: `expr.Not`, `FoundryRowValue`,
  `FoundrySelectedColumn`, `FoundryRowExpression`, `FoundrySelectedValue` and
  `FoundryPredicateValue`.

Full signatures and source fingerprints are in the evidence JSON.

## Shared files

Existing files changed relative to accepted 6b1:

- `database/orm/rows/join.go`
- `database/orm/rows/outer_join.go`
- `database/orm/rows/projection.go`
- `database/orm/rows/capability_test.go`
- `database/orm/rows/README.md`
- `database/orm/method_surface_test.go`
- `database/orm/testdata/public-methods.txt`
- `internal/ormcore/join.go`
- `internal/ormcore/expression.go`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/footprint_test.go`
- `blueprint/goway-data-layer/compilefail-mapping.tsv`, only the 19 slice rows

New shared owners are `database/orm/expr/bridge.go`,
`database/orm/rows/join_condition.go`, `database/orm/rows/subquery.go`,
`internal/ormcore/join_condition.go` and `internal/ormcore/subquery.go`.
Other additions are slice-specific regression sources, independent consumers,
probes and evidence. No existing Lane B or Lane C implementation file changes.


## Acceptance commands

All Go commands used `GOCACHE=/private/tmp/foundry-goway-codex-gocache GOWORK=off`.
Fixture tests explicitly cleared `FOUNDRY_TEST_POSTGRES_URL` and
`FOUNDRY_TEST_POSTGRES_REQUIRED`. No PostgreSQL ran and no cache was deleted.

| Command | Final result |
| --- | --- |
| `go build ./...` | Pass. Nonfatal sandbox module stat-cache warning; build completed. |
| `go vet ./database/... ./internal/...` | Pass. |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | Pass on final source. |
| Fixture: `go vet ./...` | Pass on final source, including PostgreSQL sources. |
| Fixture: `go test ./...` | Pass on final source: 229 compiler cases, SQL goldens, 47 footprint probes and model slope. PostgreSQL tests skipped. |
| `python3 tools/goway_catalogue.py` | Pass; only the 19 assigned rows changed relative to 6b1. |
| `sh tools/format-go.sh check /usr/local/go/bin/gofmt` | Pass; repository-wide `gofmt -l` is empty. |
| `make docs-check` | Pass. |
| `git diff --check` | Pass. |

The initial race run failed only on the stale public snapshot, which received
44 additive lines. The focused snapshot rerun passed. Initial fixture acceptance
also passed. Final source review then found that turning a zero scalar descriptor
into a parameter could discard the source error. The scalar boundary now carries
that error immediately, with required and nullable regression cases. Affected
races passed, followed by final build/vet, the complete requested race command and
complete fixture vet/test. Both final footprint measurements match their initial
function counts. Exact logs and hashes are retained in the evidence JSON.

## Docs notes

For the lead-owned CHANGELOG: the branch's `rows.LeftScope` and `rows.RightScope`
now accept preserved outer/cross join sides as well as inner joins. Inferred
calls remain valid. Their explicit generic parameters changed from `[L,R,M]` to
`[S,L,M]` and `[S,R,M]`; callers supplying explicit arguments must update them or
use inference. The lead approved this branch-only compatibility change after
accepting 6b1.

For the Stage 6b status/changelog: 6b2 adds row-phase ON comparison/composition,
side-owned ON filters and independent scalar/IN/EXISTS subqueries over the shared
querycore compiler. It ports 19 additional rows, with six new capability/phase
rejections. Existing Query/Table methods, existing footprint ceilings and old APIs
remain intact. Correlation and lateral remain 6b3. Transaction variants wait for
Stage 6c-owned adapters.

## Questions for the lead

No new scope decisions. The 6b1 signature approval is recorded above. Stage 6c owns
transaction record/projection adapters; 6b4 remains deferred until they exist.

## Accepted Medium review correction

`Input` and `Scalar` now implement `SelectionSource`, while join `Source`
requires a private `joinSource` marker supplied only by aliases and join chains.
`Select`, `SelectScalar`, `ExistsQuery` and the four scalar-query helpers accept
`SelectionSource`, preserving projection/subquery use without admitting an
unwrapped SELECT into any join. No SQL compiler implementation changed.

Four new compile-fail cases cover Input/Scalar × LeftJoin/RightJoin, with filtered,
limited and offset inputs. All four compile against the pre-fix source using a Go
overlay, and all four fail on the missing `joinSource` marker after the fix.
The fixture total is now 229; catalogue rows remain unchanged. All 47 existing
footprint budgets pass without ceiling changes. No new probes were needed.

The complete requested acceptance passed again after the correction, including
build, vet, races, fixture vet/tests, catalogue and formatting. PostgreSQL sources
compiled/vetted; no PostgreSQL ran. Logs: `/private/tmp/goway-6b2-review-logs/`.
The public snapshot remains additive relative to accepted 6b1: 48 added lines,
zero removals. Relative to the reviewed 6b2 patch, six subquery function signatures
now name `SelectionSource`; `Source` and `JoinTable` gain the private marker.
`Select` also accepts `SelectionSource`. Query/Table methods remain unchanged.

Shared files touched by this correction: `database/orm/rows/join.go`,
`outer_join.go`, `subquery.go`, `projection.go`, `README.md`,
`database/orm/testdata/public-methods.txt`,
`tests/fixtures/goway/compiler_cases_test.go`, and this slice's evidence/report.
Four new fixture directories use the names `subquery_input_left_join`,
`subquery_input_right_join`, `subquery_scalar_left_join` and
`subquery_scalar_right_join`.

Docs notes for the lead: Input/Scalar descriptors support selection composition;
joins require an alias or join chain so inner filters/windows retain their SELECT
boundary. Use `As` for a filtered/windowed model query in a join. No new decisions
are needed; 6b4 still waits for 6c. No commit was made.

Correction-only patch: `/private/tmp/goway-6b2-review-fix.patch`.

## Accepted Medium embedding re-review correction

Join eligibility now comes from the SQL node, alias identities, declaration
owners and scope returned together by `rowSource`. `buildJoin` delegates that
validation to the non-generic `ormcore.JoinSources` owner before joining nodes.
The existing marker remains a compile-time diagnostic only. Borrowing or promoting
it cannot authorize a different embedded source.

The boundary must contain one aliased table, or a validated alias join chain on
the left. Every source must have matching alias/owner metadata; a single alias
must match its source scope. Clauses outside that boundary are rejected. The
validator removes only the allowed Source/Joins fields from a copy and requires
the remainder to be zero, so future SELECT fields fail closed until supported.
It derives the kind from that same metadata rather than keeping a second flag.
Existing alias and subquery SQL compilation remains shared and unchanged.

The independent consumer `join_boundary_test.go` reproduces borrowed Source
markers with Input and Scalar, another level of promoted methods, and borrowed
JoinTable markers with projected alias inputs. All 26 invalid cases execute SQL
against the previous source via a Go overlay. The positive case permits valid
promoted aliases. On the corrected source, invalid cases must return
`fault.Invalid` before any SQL; existing golden tests retain alias/join-chain
behavior. The initial fixture used an incompatible aggregate/order combination;
removing that unrelated ordering made all 26 cases isolate the reported defect.

No public signature/snapshot, catalogue row, Query/Table method, dependency or
footprint ceiling changed in this correction. Existing 229 compiler cases remain.
Shared implementation touched: `internal/ormcore/join.go`. Other edits are
`database/orm/rows/README.md`, the new consumer test, and this evidence/report.

Docs notes: runtime join boundaries are validated from one source metadata value;
Go method promotion grants no authority to flatten a subquery. The previous 6b1
signature note and deferral of 6b4 until 6c remain in force. No new lead decisions.

Correction-only patch relative to the first review fix:
`/private/tmp/goway-6b2-embedding-fix.patch`.

Final re-review acceptance: build, root vet, the complete requested race suite,
fixture vet/tests, catalogue, gofmt, docs-check and diff-check all passed. All
26 invalid runtime cases reject before SQL, the promoted-alias control passes,
and all 229 compiler cases and 47 footprint budgets pass. No PostgreSQL ran;
pgsmoke sources compiled/vetted only. No commit. Command logs and hashes are in
`goway-6b2.json` and `/private/tmp/goway-6b2-embedding-logs/`.
