# Stage 9 D4: existing-parent relation loaders

Base: `748eb443`. This slice uses r11 with `GOMAXPROCS=4`, `GOFLAGS=-p=4`
and `GOWORK=off`. No commit.

## Migrated

- `model_relations_postgres_test.go`: typed ORM assignments, direct relations,
  nested/self and natural-key loads, and query-preserving `LoadFrom` /
  `LoadMissingFrom`.
- `model_through_postgres_test.go`: typed pivots, independent target/pivot scopes,
  nested target/pivot loading, nullable and duplicate keys, and the intentionally
  ambiguous alternate-key relation built with `ManyToManyOf`.
- `model_aggregates_postgres_test.go`: `UserValues`, `expr.Related`, `orm.Pivot`,
  typed correlated/conditional filters and the same per-slot batches and budgets.

All existing checks remain: 77 diagnostic calls, every explicit query count and
limit, every fixture SQL literal, exact decimal/float/null results, loaded-empty
and missing behavior, cardinality (including filtered-empty aggregates),
cancellation, release after malformed rows, and no partial results or caller
mutation. Setup assignments preserve replacement versus omission, including the
disabled user's status. No new raw SQL or substitute key predicate was added.

The editor catalogue now targets the handwritten relation/pivot/aggregate
namespaces and explicitly inspects both custom-limit loaders. Ready audit,
soft-delete, hook and retrieval-observer examples also moved to their ORM
sources. Audit getter/mutator documentation remains a separate semantic probe;
hook editing checks both nullable Set/SetNull and WriteSet/WriteUnset/WriteGet.

## Acceptance

Targeted root/editor build, vet and race checks pass, as do twelve compile-fail
cases, the catalogue (577 old rows / 569 cases), four unchanged footprint probes,
generation freshness, docs-check, gofmt and diff-check. The explicit
`make agent-smoke` run passed 479/500 probes and failed 21 during gopls shutdown.
All 21 passed serial retries with unchanged assertions and deadlines: all 500
probes are covered across the segmented run. Exact commands, failed probe names,
logs and source hashes are in `goway-stage9-d4-loaders.json`.
PostgreSQL sources are compiled and vetted only; the lead runs their live checks.
No schema, codecs, manifests, OpenAPI, TypeScript, generated output, module pin,
ownership manifest or Makefile changed. No API, method-surface, catalogue row or
footprint ceiling was added or changed.

## Remaining D4 work and blockers

1. Shared model helpers: the preserved root checks for `User.CurrentEmailDraft`
   and `Product.CreatorDraft` still consume legacy return types. Lane C owns
   `models` and `catalog`; migrate those helper contracts there before cutting
   over these two assertions. They are not replaced with constructed snapshots.
2. Thirty-four legacy SQL/model editor probes still point at Lane C consumers:
   `advancedqueries`, `comparisonqueries`, `ctes`, `expressionqueries`,
   `filterqueries`, `framequeries`, `jsonqueries`, `keyqueries`, `recordqueries`,
   `recursivequeries`, `scalarqueries`, `setqueries`. Their inspected examples
   still use legacy APIs at this base; the source owners must supply the matching
   migrated assertions/anchors. The exact probe list is in the evidence JSON.
3. `typed-automatic-mutator` still inspects the generated call to the consumer's
   `MutateEmail`. Keep its completion/signature assertion until Lane B supplies a
   handwritten call site during `mutatorqueries` cleanup.
4. Lead barrier: remove obsolete directives, generated declarations and legacy
   compiler programs/diagnostics only with their owners' cutovers and serial
   Makefile generation, preserving mixed DTO/config/enum output and manifests.
   No generated files or owned manifests were edited here.
5. Oracle freeze: retain `goway/oldmodels` and old/new goldens until the explicit
   Stage 8d decision. The frozen legacy footprint evidence and all existing
   ceilings remain unchanged. The four consumer probes pass with unchanged
   measurements (1159 / 139 / 233 / 1085 functions).

The root compiler harness, retained module-pinned config generator check and
TypeScript gate wiring need no further changes at this base. DTO/config/enum
editor probes remain intentionally generated; they are not retirement blockers.

## Shared files and Docs notes

Shared file touched: `internal/agent/gopls_acceptance_test.go`. Other changes are
the three root test files, the consumer README and this slice's evidence.
Lead-owned docs are untouched. The previous D4 custom-loading-limit blocker is
closed; generator retirement still waits for the supplier and barrier work above.
