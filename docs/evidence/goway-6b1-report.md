# Lane A, Stage 6b1

Base: `a5f30a6264aaaa24e671ddbc5f37df06e5f9e996`. Slice 1 only. No commits,
PostgreSQL execution, new dependencies or edits to lead-owned status documents.
Current command results and source hashes are in [the evidence JSON](goway-6b1.json).
A complete patch including all new files is at `/private/tmp/goway-6b1.patch`;
plain `git diff` omits these untracked files because staging is prohibited.

## Inventory

The old `database/query` API provides:

- `As` for model/projection/CTE records, distinct typed alias tags and names, and
  source boundaries that retain filters, ordering and windows.
- `InnerJoin`, `LeftJoin`, `RightJoin`, `FullJoin` and `CrossJoin`. Join kind is part
  of scope identity. `LeftScope` and `RightScope` preserve sides;
  `NullableLeftScope`, `NullableRightScope` and `LeftNullableScope` retain missing
  rows and nullability through chains. The right input is one aliased record.
- Key `On`, `OnAnd`, `OnOr`, side-owned `WhereLeft` and `WhereRight`, row-expression
  equality/range/LIKE comparisons, nullable variants and distinctness comparisons.
- `InQuery` and `InNullableQuery` with exact single-column types; `ExistsQuery`
  anchored to an outer scope; selected and row scalar subqueries. An empty scalar
  is NULL, multiple rows remain a database cardinality error, and explicitly
  nullable variants preserve one layer. Ordinary subqueries cannot erase locks.
- `Correlate(outer, inner)`, explicit outer/inner scope lifting, nullable lifting,
  scoped predicates, existence, membership, scalar values and complete records.
  Correlated sources cannot execute independently or become ordinary sources.
  Source visibility and shadowing are validated at the SELECT boundary; nested
  correlations can declare a containing correlation as their outer source.
- `AsLateral` for correlated records/projections and cross/inner/left lateral
  joins. The required outer input stays in the type. Lateral sources cannot
  execute alone or enter ordinary joins, aliases or CTEs. LEFT LATERAL widens the
  right record; optional ON conditions refer to the lateral output alias.
- Separate transaction-required aliases, joins, subqueries, correlations and
  lateral families. They retain transaction executors and cannot convert into
  ordinary source/query families. Nullable sides cannot become lock targets.

Before this slice, `orm/rows` had model-only `As`, `Scope`, `On`, `InnerJoin`,
inner-only `LeftScope`/`RightScope`, declared complete projections and scalar
selection. It already reused `orm/expr` and `querycore.AliasNode`/`JoinNodes`.
It had no outer/cross joins, nullable model scopes, general subqueries,
correlation, lateral or transaction-only row source families.

Exactly 100 catalogue rows belong to Stage 6b:

| Feature | Rows |
| --- | ---: |
| Ordinary joins, aliases, typed scopes and nullable lock targets | 25 |
| Ordinary scalar, IN, EXISTS and locked subquery rejection | 11 |
| Correlation | 16 |
| LATERAL | 15 |
| Transaction-only variants across those families | 33 |

The evidence JSON lists every case in its group. Slice 1 ports 15 rows;
85 remain for later slices. Four additional cases pin nullable-scope conversion,
wrong nullable side and required-versus-nullable column entry points.

## Slice plan

1. Join foundations: share ownership/check propagation, add outer/cross join kinds,
   distinct scopes and nullable model-field lifting, and preserve inner joins.
2. Complete ON row-expression conditions and independent scalar/IN/EXISTS
   subqueries. Reuse `orm/expr` codecs/phases and the shared SELECT/subquery compiler.
   Extend the correlation owner used by `relation_exists.go` rather than adding a
   separate EXISTS compiler.
3. Explicit outer/inner correlation, correlated values/records and cross/inner/left
   LATERAL, preserving non-executable intermediate sources and required parents.
4. Transaction-required aliases, joins, subqueries, correlation and lateral; retain
   executor and lock-target restrictions. Coordinate row-record adapters with 6c.

## Slice 1 implementation

`ormcore.JoinSources` now owns reference validation, owner snapshots and deferred
checks around `querycore.JoinNodes`. Existing inner joins use that same path.
`ormcore.Rescope` validates the selected side and runtime declaration. No SQL
compiler or relation-EXISTS implementation is duplicated or changed.

New public types are `rows.Left`, `Right`, `Full`, `Cross` and the corresponding
`*Joined` types, plus `expr.NullableScope`. They contain non-generic state and
phantom scope fields, with direct method shells and no embedding. Join-kind
markers prevent explicit conversions that would erase nullability.

New functions are `rows.LeftJoin`, `RightJoin`, `FullJoin`, `CrossJoin`,
`NullableLeftScope`, `NullableRightScope`, `LeftNullableScope`, and
`expr.AtNullable`, `AtNullableValue`, `FoundryNullableScope`,
`FoundryNullableScopeData`. Nullable bridges validate issued expression-family
capabilities; zero capabilities cannot read or adopt metadata.

`LeftScope` and `RightScope` now accept sealed preserved-side interfaces so the
same calls support inner, cross and preserved outer sides. Inferred existing
calls remain valid. The generic parameter order changes from `[L,R,M]` to
`[S,L,M]` and `[S,R,M]`; callers supplying explicit old type arguments need to
use inference or update them. No explicit calls exist in the inspected consumers.
No Query or Table methods change.

`AtNullable` accepts required columns and widens selected results. `AtNullableValue`
accepts already nullable columns and keeps one layer. Present-value comparisons,
exact model/key/result types, row phases, references and codecs remain intact.
Nullable scopes cannot enter `expr.At` or convert to preserved scopes.

All 15 ported rows have real compile-fail fixtures and diagnostics. Six old/new SQL
and binding goldens cover all join kinds and a nullable chain, filtered/windowed
inputs and nullable results. Unit tests cover actual NULL hydration, runtime
wrong-declaration/alias rejection, nil inputs and zero capabilities. PostgreSQL
sources cover unmatched left/right/full rows, cross/inner multiplicity, soft-delete
filters, nullable predicates and a chained join. The lead runs them.


## Footprint and public API snapshot

The dedicated archive probes measure compiled T/t function symbols, not runtime
speed. Native Go 1.27.1 on darwin/arm64, warm shared cache, one export archive per
probe; the existing fixture guard repeats the same symbol count.

| Probe | Functions | Ceiling, ceil(measured × 1.15) | Archive bytes |
| --- | ---: | ---: | ---: |
| `outer_join` | 1,668 | 1,919 | 7,832,384 |
| `nullable_join_chain` | 1,695 | 1,950 | 7,923,594 |

All pre-existing baselines and ceilings are retained. No runtime speed claim is
made. The new probes construct complete projected queries, including required
and already nullable columns and a nullable scope through a second join.

The public snapshot adds exactly 35 lines and removes none. Each addition is:

- Value and pointer entries for `rows.Cross`, `rows.CrossJoined`, `rows.Full`,
  `rows.FullJoined`, `rows.Left`, `rows.LeftJoined`, `rows.Right`,
  `rows.RightJoined` and `expr.NullableScope`: 18 lines.
- Function signatures for `rows.As`, `rows.Scope`, `rows.On`, `rows.InnerJoin`,
  `rows.LeftJoin`, `rows.RightJoin`, `rows.FullJoin`, `rows.CrossJoin`,
  `rows.LeftScope`, `rows.RightScope`, `rows.NullableLeftScope`,
  `rows.NullableRightScope`, `rows.LeftNullableScope`, `expr.AtNullable`,
  `expr.AtNullableValue`, `expr.FoundryNullableScope` and
  `expr.FoundryNullableScopeData`: 17 lines. Six of these pin previously unpinned
  existing functions. Their complete signatures are in the snapshot and JSON.

## Shared files

Existing shared files touched:

- `database/orm/rows/join.go`
- `database/orm/rows/capability_test.go`
- `database/orm/method_surface_test.go`
- `database/orm/testdata/public-methods.txt`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/footprint_test.go`
- `blueprint/goway-data-layer/compilefail-mapping.tsv`, only the 15 slice rows

New shared owners are `internal/ormcore/join.go` and
`database/orm/expr/nullable_scope.go`. Remaining additions are slice-specific rows
code, tests, consumer cases/probes, package documentation and evidence. No existing lane B
or lane C implementation file changes.


## Acceptance commands

Go commands used `GOCACHE=/private/tmp/foundry-goway-codex-gocache GOWORK=off`.
Fixture test runs explicitly cleared the PostgreSQL URL and required flag, so no
PostgreSQL test executed. No cache was deleted.

| Command | Result |
| --- | --- |
| `go build ./...` | Pass. Nonfatal sandbox warning for a module stat-cache write; exit 0. |
| `go vet ./database/... ./internal/...` | Pass. |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | Pass in segments. Initial run passed every package except the stale public snapshot; focused `go test -race ./database/orm -run '^TestPublicMethodSurface$'` passed after its 35 additive lines. |
| Fixture: `go vet ./...` | Pass after fixing a conflicting `rows` import alias; final-source rerun also passes. |
| Fixture: `go test ./...` | Pass in segments. SQL goldens, runtime tests, all 45 footprint probes and 197 compiler cases passed initially. Three expected-diagnostic strings were corrected, then those three cases passed in the focused rerun below. All 200 compiler cases are covered. |
| Fixture: `go test ./... -run '^TestInvalidPublicContractsFailCompilation$/(join_outer_preserved\|join_full_scope\|join_right_scope)$'` | Pass. |
| `python3 tools/goway_catalogue.py` | Pass. Only the 15 assigned rows changed. |
| `sh tools/format-go.sh check /usr/local/go/bin/gofmt` | Pass. Repository-wide `gofmt -l` is empty. |
| `git diff --check` | Pass. |
| `make docs-check` | Pass. |

The shared-cache export/`go tool nm` measurement supplies the new budgets. Root
`go vet ./...` and a filtered root `go test ./... -run ...` also completed because
of two command-directory mistakes. The filtered root run executed no matching
tests and is not used as fixture evidence. Correct fixture-module commands are
recorded above. Exact logs, corrections, SHA-256 digests and source fingerprints
are retained in the JSON. No new runtime-source fixes followed the passing
runtime checks.

## Docs notes

For the lead-owned program status/changelog: Stage 6b1 adds outer/cross joins and
nullable typed scope foundations over the shared alias/join compiler. It ports
15 of 100 Stage 6b catalogue rows and adds four rejection cases. The package recipe
is `database/orm/rows/README.md`. General subqueries, ON expression composition,
correlation, LATERAL and transaction-only variants remain planned slices.

## Questions for the lead

- Resolved by the lead: keep the broadened preserved-scope signatures. Inferred
  calls stay valid; explicit generic arguments change as documented above.
- Slice 4 needs transaction record/projection adapters also planned in Stage 6c.
  Confirm whether that shared adapter work belongs to Lane A or the 6c owner.


## Integration on 2cc60e8

The lead approved the broadened preserved-scope signatures. The four conflicted
files retain both lanes' rows, surface entries and probes. Ordered input aggregate
capabilities use the approved private framework marker; the method-count guard
counts exported methods (limit 25) while retaining private-signature reachability
checks. No footprint ceiling changed.

Combined build, vet, races, fixture vet/tests, catalogue, release-tools, docs and
formatting checks pass. The merged fixture has 271 compiler cases and 50 probes.
Outer joins measure 1,738/1,919 functions/ceiling; nullable chains measure
1,765/1,950. PostgreSQL cases compile/vet and skip without configuration. The
JSON's integration entry records current logs, counts and source hashes; the
lane-only results above remain historical evidence.
