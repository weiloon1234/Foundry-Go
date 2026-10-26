# Stage 12 pilot migration-guide additions

The accepted 8d2 work remains intact. This addition changes the migration guide
and adds `tests/fixtures/goway/migration/pilot_test.go`; it changes no runtime API,
generated output or external pilot specification.

## Entries and compiled examples

All eight inventory rows are covered, plus the potentially masked group/relation
filter calls. Nine excerpt regions cover ORM/DTO/rows declarations, datatable
columns, Source and Stable, group/enum/relation filters, the custom nullable filter,
the joined schema, joined/correlated construction, Countries and generic Lookup.
The existing excerpt guard checks every new Go block verbatim against compiled
source. New before blocks are documentation-only `text` fences.

The source tests validate active versus archived SQL and tenant binding, compile
the joined/correlated projection, and validate aggregate, enum and related filter
declarations. Executor-dependent Countries and validation examples are type-checked
without opening a database. Existing DTO descriptors and authorization remain
caller-owned in the Spec examples. The guide links datatable, Countries, validation
and outer-join guidance. The stale assertion that LoadFrom is unavailable is also
corrected against its shipped implementation.

## Codemod notes

Apply mechanical rewrites only after resolving the model's native declaration and
its stored type/capabilities. Preserve existing codecs, labels, Restrict/AllowLike,
authorization, ordering directions and export settings.

| Inventory row | Classification and exact rewrite |
| --- | --- |
| 158 field sorts | Mechanical: `SortBy(query.Value(f.X))` → `SortBy(expr.Select(expr.Value(f.X)))`, after rebinding `f` to the ORM declaration. |
| 118 Where calls | Capability-dependent mechanical: text `Where(codec, f.X)` → `Where(codec, expr.TextRow(f.X))`; equality/enum → `Where(codec, expr.CompareRow(expr.Value(f.X)))`; ordered numeric → `Where(codec, expr.OrderRow(expr.Value(f.X)))`. Do not infer enum text/range support from its underlying Go type. |
| 35 Stable lists | Mechanical for model fields: `[]query.ProjectionOrder[M]{f.ID.Asc()}` → `[]expr.ProjectionOrder[M]{expr.Value(f.ID).Asc()}`. Map an unnamed primary carrier to `f.Key()`. Preserve all directions and tie-breakers. Joined/grouped uniqueness requires review. |
| 35 Source/projection uses | Manual: preserve DTO identity/tags/descriptors, replace projection directive with retained DTO generation, declare rows schema and replace generated selections with `rows.Select(rows.From(source), schema, rows.Map(...))`. Return `rows.Query[S,R]`; use `MapSelected` for computed values. Keep OnlyTrashed/WithTrashed and trusted tenant filtering. |
| 6 NullableWhere calls | Capability-dependent mechanical: nullable text → `NullableWhere(codec, expr.TextNullableRow(f.X))`; nullable equality/enum → `NullableWhere(codec, expr.CompareNullableRow(expr.Value(f.X)))`; nullable ordered numeric → `NullableWhere(codec, expr.OrderNullableRow(expr.Value(f.X)))`. Keep the present-value codec. |
| Countries factories/fields/drafts | Factories/fields mechanical: `QueryFoundryCountries()` → `Countries.Query()`, `CountryFields()` → `Countries`; Find/Update move to `Countries.Find/Update`. Chained draft setters become separate `Countries.X.Set(v)` assignments. A reused/conditionally built draft needs manual omission/null review. Keep natural Code keys and caller transactions. |
| 2 non-field sorts | Manual scope construction: joined value → `expr.Select(expr.At(scope, field))`; correlated row scalar → `expr.Select(rows.CorrelatedScalarRowQuery(selected))`, or `CorrelatedScalarNullableRowQuery` for an already nullable inner value. Keep inner order/limit and one nullable layer. |
| 1 Lookup helper | Mechanical signature after source migration: `query.ModelQuerySource[M]` → `orm.Query[M]`; `query.KeyField[M,string]` → `orm.KeyColumn[M,string]`; `validationdb.Lookup` is unchanged. Keep the application's canonicalizing lookup wrapper and WithTrashed policy. |
| Masked Having/NullableHaving | Keep constructors; migrate their operands to native aggregates and preserve grouped source mappings. Integer Sum now spells its exact nullable decimal result in the native type. Aggregate semantics and selected aliases require manual review. |
| Masked Related/custom adapters | Manual: callbacks return `expr.Predicate`, with `expr.Has` for declared relations or scoped correlated EXISTS. Preserve null/absence and negation behavior, operator lists and child authority scopes. |

The example rule helper receives the existing username rule as an argument. The
codemod need not change an application's own `Username()` helper or its normalization.

## Gaps

None found for the supplied inventory. No new framework signature is required.
The joined example uses an INNER JOIN, so missing joined countries are excluded;
its nullable official-name filter handles missing/NULL lookup results explicitly.
Outer joins require nullable scopes and DTO fields as described in the guide.

## Acceptance

Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r16`,
`GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`.

- `go test ./migration` from `tests/fixtures/goway`: pass, including
  `TestGuideSnippets`, `TestLifecycleGuideSnippets` and the new source test.
- `make docs-check`: pass.
- `gofmt` on `pilot_test.go`, followed by an empty `gofmt -l`: pass.
- `git diff --check`: pass.

An initial compile found that row negation is the package function `expr.Not`,
not a Predicate method; the example was corrected and the package rerun passed.
No commit was made.
