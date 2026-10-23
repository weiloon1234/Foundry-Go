# Lane A, Stage 6b3

Correlation and cross/inner/left LATERAL are implemented on the accepted 6b2
worktree. No commit, dependencies, PostgreSQL execution, or lead-owned document
edits. The source fingerprints and full surface additions are in
[the evidence JSON](goway-6b3.json). The incremental delivery patch is
`/private/tmp/goway-6b3.patch`, based on the accepted 6b2 embedding fix; it includes
new files that plain `git diff` omits.

## Inventory and slice plan

The old `database/query` families cover typed inner/left/right/full/cross joins,
alias boundaries, preserved/nullable scopes, ON comparisons, scalar/IN/EXISTS,
explicit enclosing correlation, and cross/inner/left LATERAL. Correlated and
lateral intermediates cannot execute independently or enter ordinary source
families. The original Go-way surface had `rows.InnerJoin`, `rows.As` and
`rows.Scope`; accepted 6b1/6b2 added outer/cross joins, nullable scopes, ON
comparisons, independent subqueries and structural join-boundary validation.

The original 100 Stage 6b rows group as 25 joins/aliases/scopes/nullable lock
targets, 11 ordinary subqueries, 16 correlation, 15 LATERAL and 33 transaction
variants. This slice ports all 16 correlation rows and 14 LATERAL rows. Stage 6b
now has 64 ported and 36 pending rows. `lateral_record_cte` remains pending because
the Stage 6c CTE API is absent; no missing-symbol test substitutes for it.

The four-slice plan remains:

1. Join foundations and nullable scopes — accepted.
2. ON comparisons and independent scalar/IN/EXISTS — accepted, including fixes.
3. Correlated values/records and LATERAL — this delivery; CTE rejection awaits 6c.
4. Transaction-required variants and nullable lock restrictions — after 6c's
   transaction record/projection adapters, as the lead decided.

## Implementation and API

`Correlate` retains outer and inner visibility plus runtime declaration identity.
Explicit scope lifting admits only declared enclosing sources, including nested
ancestors. Shadowed SQL names, foreign declarations, zero descriptors and missing
outer scopes fail closed. The correlated descriptors have no ordinary source or
execution getter. `AsLateral` retains a complete model or projected record inside
a derived SELECT, and join construction validates the required preceding scope.
LEFT LATERAL widens the right result; unsupported right/full LATERAL cannot enter
the public join functions. Existing structural alias-boundary checks remain in
force, including promoted-method regressions from 6b2.

The shared `querycore` correlation, subquery, relation-EXISTS, alias and join
compilers are unchanged. `ormcore` supplies nongeneric metadata composition;
`orm/expr` supplies typed values and phases through existing validated capability
families. No new seals or SQL compiler paths were introduced. `From(modelQuery)`
now represents an unselected input; ordinary subqueries still choose their full
record through the existing compiler adapter. Correlated source EXISTS uses
SELECT 1, while explicit scalar/record selections preserve aggregate cardinality.

The public snapshot adds 65 lines and removes none. Every addition is listed
below; exact signatures are retained in the JSON and snapshot.

- Value/pointer type entries for `Correlation`, `CorrelatedSource`,
  `CorrelatedScalar`, `CorrelatedRecord`, and `LateralSource`: 10 lines.
- `ScopeSource` and its sealed `correlationScope` signature: 2 lines.
- Direct `Where`, `OrderBy`, `Limit`, `Offset`, and `Exists` methods on each of
  `CorrelatedSource`, `CorrelatedScalar`, and `CorrelatedRecord`, in value and
  pointer method sets: 30 lines.
- Functions `Correlate`, `ModelScope`, `OuterScope`, `InnerScope`,
  `OuterNullableScope`, `InnerNullableScope`, `SelectCorrelatedScalar`,
  `CorrelatedScalarQuery`, `CorrelatedScalarNullableQuery`,
  `CorrelatedScalarRowQuery`, `CorrelatedScalarNullableRowQuery`,
  `InCorrelatedQuery`, `InNullableCorrelatedQuery`, `SelectCorrelatedRecord`,
  `ProjectCorrelated`, `AsLateral`, `LateralScope`, `CrossJoinLateral`,
  `InnerJoinLateral`, `LeftJoinLateral`, `At`, `AtNullable`, and `AtNullableValue`:
  23 lines. All belong to `rows`.

No Query/Table method or existing public signature was added or changed.
The `rows.At` family reads declared projection fields using their retained value
codec and exact declaration identity; model fields keep using `expr.At`.

## Regression rows and footprint

The 30 ported rows are the 16 `correlation_*` rows and all 14 non-CTE ordinary
`lateral_*` rows in the original catalogue. Only those rows changed. The evidence
lists each name. Six extra rejection cases cover ordinary scalar/selection
escapes, scope conversion, unbound lateral selection and right/full lateral use.
The complete fixture registry now has 265 cases.

Runtime tests cover nested visibility, nullable lifting, foreign declarations,
shadowing, lazy validation, zero-value failure, windows, complete projected
records, and invalid forward/self/missing LATERAL scopes. Six old/new SQL,
binding and result goldens cover correlation, nested correlation, cross, inner,
left and ON-filtered LATERAL. Pgsmoke sources cover latest-child selection,
unmatched parents, ON preservation, soft deletes, correlated COUNT/IN/EXISTS and
scalar cardinality error 21000. Those sources were compiled and vetted only.

Native Go 1.27.1 darwin/arm64 archive T/t symbols, using the requested shared cache:

| New probe | Measured functions | Ceiling, ceil(measured × 1.15) |
| --- | ---: | ---: |
| correlation | 2,752 | 3,165 |
| lateral | 2,852 | 3,280 |

All 47 previous baselines/ceilings are unchanged; there are now 49 probes. These
are compiled-size guards, with no runtime performance claim. The probes exercise
nested scalar/IN/EXISTS and model/projected nullable LATERAL respectively.

## Shared files

Existing files changed relative to accepted 6b2:

- `database/orm/rows/declaration.go` — retain field codec through a thin closure.
- `database/orm/rows/projection.go` — share scoped ordering with new descriptors.
- `database/orm/rows/subquery.go` — retain the unselected model-input boundary.
- `database/orm/rows/README.md` — correlation/LATERAL recipe and restrictions.
- `database/orm/method_surface_test.go` and `database/orm/testdata/public-methods.txt`.
- `tests/fixtures/goway/compiler_cases_test.go` and `footprint_test.go`.
- `blueprint/goway-data-layer/compilefail-mapping.tsv` — only the 30 assigned rows.

New shared owner: `internal/ormcore/correlation.go`. Other additions are dedicated
rows implementations/tests, consumer goldens, pgsmoke, compiler cases, probes and
evidence. No lifecycle, aggregate/group/having, raw-fragment, or relation
compilation code changed.

## Acceptance

All Go commands use
`GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2 GOWORK=off`.
Fixture tests explicitly clear the PostgreSQL URL and required flag. No cache was
deleted. Exact logs, statuses, corrections and SHA-256 hashes are in the JSON.

| Command | Final result |
| --- | --- |
| `go build ./...` | Pass. |
| `go vet ./database/... ./internal/...` | Pass. |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | Pass. |
| Fixture: `go vet ./...` | Pass, including pgsmoke sources. |
| Fixture: `go test ./...` | Pass; 265 compiler cases, 49 footprint budgets, all goldens. PostgreSQL skipped. |
| `python3 tools/goway_catalogue.py` | Pass. |
| `sh tools/format-go.sh check /usr/local/go/bin/gofmt` | Pass; repository-wide `gofmt -l` clean. |
| `git diff --check` | Pass. |
| `make docs-check` | Pass. |

Initial failures were the expected additive API snapshot, three compiler diagnostic
strings, and the SELECT 1 parity issue. The first SELECT correction covered aliases;
the final correction normalized model inputs too. Final checks include both fixes.

## Docs notes

For the lead-owned changelog/status: Stage 6b3 adds explicit enclosing correlation,
nullable scalar/IN/EXISTS composition and model/projected cross/inner/left LATERAL
using the existing shared SQL compiler. Thirty more Stage 6b rows are ported;
CTE rejection awaits 6c, and transaction variants remain deferred until 6c.
The package recipe is `database/orm/rows/README.md`.

Carry forward the approved branch-only compatibility note: 6b1 broadened
`LeftScope`/`RightScope` from `[L,R,M]` to `[S,L,M]`/`[S,R,M]`. Inferred calls
remain valid; explicit old type arguments must be updated. CHANGELOG is untouched.

## Questions for the lead

No unresolved API questions. Carry `lateral_record_cte` into the 6c integration
checkpoint, and run the new pgsmoke source there. Slice 4 remains blocked on the
already agreed 6c transaction adapters.
