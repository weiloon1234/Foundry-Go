# Go-way pilot ergonomics: proposal (lane D)

Base `f713458f`. Planning only; nothing below is shipped unless marked **exists**. Pilot references are to
`/Users/weiloon/Projects/ooo-orm-pilot`. No footprint measurements were taken: the `q_with` ceiling claims
are qualitative and any API addition must be gated by the existing probe before merging.

## 1. Locked find by key

**Exists.** `Table.Find/Require` (`database/orm/query.go:69,74`) take `database.Executor` and build
`Query().Where(Key().Eq(key))`. Locks live only on `LockedQuery` (`query.go:173,180,213-233`), whose
`First/Require/All` take `*database.Tx` and call `lockContext`. `KeyLookup.Find` (`lookup.go:70`) takes a plain `Query`,
so it cannot lock. The pilot spelling `APIKeys.Query().Where(APIKeys.Key().Eq(id)).ForUpdate().Require(ctx, tx)`
(`api_key_service.go:~172`) is the shipped idiom and the key predicate is still mandatory and typed.

**Proposal: no API change; add a guide recipe.** Locked key reads need two modes (update/share) times
Find/Require, so four Table methods for one saved `.Where(...)`. A parameter-free `RequireForUpdate` would also
hide the lock mode from the call site. If the lead still wants it:
`func (t Table[M, K]) RequireForUpdate(ctx, tx *database.Tx, key K) (M, error)` in `database/orm/query.go`,
delegating to the line above (`*database.Tx` keeps "locks need a transaction" a compile error). Risk: methods on
the generic `Table` are the most widely instantiated surface (footprint probe required). Tests: real PostgreSQL
lock test plus compile-fail with `database.DB`. Docs: the recipe in the model-queries guide, one line.

## 2. Model reference/identity

**Exists.** `orm.IdentityOf(table, key)` (`identity.go:25`) with `ReferenceOf(m)` (`identity.go:~55`),
`Reference(key)`, `Parse`; guides show it (`docs/guides/audit.md:42`, `authentication.md:27-30`). Go cannot attach
`FoundryReference` to a model from a declaration option (methods need a receiver declaration), so a
declaration-time option is infeasible; the 3-line handwritten method (`api_key_model.go:57-66`) is inherent.

**Proposal (small, optional):** `func (t Table[M, K]) Identity() ModelIdentity[M, K] { return IdentityOf(t, t.Key()) }`
in `database/orm/identity.go`. It removes the `Xs.Table, Xs.Key()` repetition and makes the "key from another
declaration" mismatch (which `Validate` currently catches at runtime, `identity.go:32`) unrepresentable. Footprint:
one tiny method, no new generic type. **Required: the guide entry**: a "Reference methods on a handwritten model"
snippet (var identity, `FoundryReference`, `FoundryIdentity`) in `docs/guides/model-declarations.md`, linked from
`query-build-resources.md:60` and the audit/auth guides. Tests: a consumer-fixture compile of the snippet.

## 3. Sort adapter asymmetry

**Exists.** `RowComparison` (`expr/filter.go:34-58`) has only `Eq/Ne/In`; `OrderedRowExpression.Value()`
(`row.go:77`), `TextRowExpression.Value()` (`text_row.go:32`) and `RowExpression.Value()` (`value.go:57`) exist.
`Selected` (`value.go:32`) is the selected-phase type so a selected value cannot be a row predicate; `SortBy` takes
`expr.Selected[S, V]` (`datatable/column.go:109`) and `Stable` takes `[]expr.ProjectionOrder[S]`
(`datatable/declaration.go:39`). `ProjectionOrder` is sealed by an unexported method (`expr/keys.go:31-34`).
Key finding: **`expr.Order[S]` already implements `ProjectionOrder`** (`keys.go:33`) and `expr.Expression` has
`Asc/Desc` (`expression.go:77,82`), so the stable key is already
`expr.Value(k.Key()).Asc()` with no `Select`. The pilot's `expr.Select(expr.Value(k.Key())).Asc()`
(`api_keys_datatable.go:~66`) is needlessly long, not required. (Inferred from types; a one-line probe should
confirm before documenting.)

**Proposal:** (a) docs: use `expr.Value(k.Key()).Asc()` for `Stable`; (b) add
`func (e RowComparison[S, V]) Value() Selected[S, V]` in `expr/filter.go`, mirroring the sibling adapters, so an
enum can use the same `cmp := expr.CompareRow(expr.Value(k.Purpose))` for filter and `SortBy(cmp.Value())`. No
guarantee lost: enum sorting is already reachable via `expr.Select(expr.Value(enum))`; `.Value()` yields the same
`Selected`, and the comparison type still lacks ordered/text row operations. Also add
`NullableRowComparison.Value()` (returns `Selected[S, value.Nullable[V]]`, as `row.go:141`). Footprint: two
methods on small generic structs, instantiated only per enum column used. Tests: unit and a compile-fail that
`RowComparison` still has no `Lt/Like`; datatable guide row updated (`datatable.md:62-72`).

## 4. Datatable schema with isolated test databases

**Exists.** `DefaultConfig().Schema = "public"` (`datatable/manager.go:70`); `read` runs
`SET LOCAL search_path TO "<Schema>", pg_temp` (`manager.go:312`), deliberately overriding the connection path, and
`Validate` requires a valid identifier (`manager.go:87`). `Scope.Schema()` and `Scope.Config()`
(`testkit/postgres/scope.go:46,74`) exist; the shipped datatable test already does `config.Schema = fixture.Schema`
(`datatable/datatable_postgres_test.go:88`).

**Proposal: docs only.** Pinning the path is a safety property (a per-request deterministic schema), so "empty =
inherit" would weaken it, and `testkit` must not import `datatable` (dependency direction). Add to
`docs/guides/datatable.md` and `postgresql.md` testing section:
`config := datatable.DefaultConfig(); config.Schema = scope.Schema()` and the 42P01 symptom. Zero code risk.

## 5. Generator per-package type-check

**Exists.** The generator parses and then type-checks the complete package overlay with a single
`types.Config{Importer}` and no `Error` callback (`internal/generate/load.go:129-152`), so the first error in any
file, including a sibling not yet migrated, fails the run with "check generated package overlay". The generated
DTO descriptors the datatable file references are themselves produced from that same checked package; the
declaration-first pass explicitly leaves "ordinary parse/type errors" to the overlay (`graph.go:203`,
`declarations.go:20`).

**Assessment.** Tolerating unrelated errors would need `Config.Error` collection plus proof that the DTO's
transitive closure is error-free; partial type info (invalid types) can silently produce a wrong schema, which
conflicts with "hydrate complete or fail" and the generator's guarded-publication rules. **Recommendation:
documented constraint** ("convert a package atomically; a package must type-check except for stale generated
files"). Feasibility of tolerance: medium effort, high risk; not advised. Docs: the migration guide / generate
AGENTS cross-link; a diagnostic hint naming the failing file is the only cheap improvement (error text already
includes the position).

## 6. Drafts as assignment slices

**Exists.** No optional-assignment helper. Column `Set` takes a concrete `V` (`column_methods.go:36,157,309,...`),
`SetNull` is separate for nullable columns (`:358,550`); only `Write` drafts read optionally
(`WriteGet`, `write_draft.go:64`, signature `setter func(I) Assignment[M]`). In the pilot the conditionals are on Go
locals (`hasAgent`, `hasExpiry`, `api_key_service.go:150-175`), not on `value.Optional`, so an Optional-based helper
would not even apply there.

**Proposal: no change now.** If repetition grows with real `value.Optional` inputs:
`func Present[M, I any](in value.Optional[I], set func(I) Assignment[M]) []Assignment[M]` (owner `database/orm`,
beside `WriteGet`), used as `append(a, orm.Present(in.AgentID, k.AgentID.Set)...)`. It returns none when omitted and
`set(v)` when set (zero preserved), so omitted/value stay distinct; null needs a separate nullable form, which is
why it is deferred rather than hiding null semantics. Tests: omitted, zero, value.

## Summary

| # | Item | Recommendation | Effort | Risk |
| --- | --- | --- | --- | --- |
| 1 | Locked find by key | Docs recipe; no new API (optional `Table.RequireForUpdate`) | XS docs / S code | Low docs; Medium code (footprint on `Table`) |
| 2 | Reference/identity | Guide entry (required) plus optional `Table.Identity()` | S | Low |
| 3 | Sort asymmetry | Add `RowComparison.Value()` (and nullable); document `expr.Value(k.Key()).Asc()` for `Stable` | S | Low |
| 4 | Datatable schema in tests | Docs only (`config.Schema = scope.Schema()`) | XS | None |
| 5 | Generator per-package check | Documented constraint; do not tolerate errors | XS docs | High if changed |
| 6 | Optional assignments | No change; defer `orm.Present` until Optional inputs recur | XS / S | Low |

Overall: items 3 and 2 (`Table.Identity`, guide) are the only worthwhile code; 1, 4, 5, 6 are documentation.
Gate both code additions on the `q_with` footprint probe before merge, and confirm `Stable` accepts a row-phase
`Order` with a one-line probe. The pilot friction is mostly undiscovered idioms, so a "pilot recipes" guide section
covers most of the value.
