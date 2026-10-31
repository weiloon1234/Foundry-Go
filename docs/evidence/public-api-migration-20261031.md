# Public API documentation and remaining D2 migration

The [baseline inventory](public-api-inventory-20261031.md) lists every exported
method on a private receiver and documentation counts for all 160 importable
packages. This report describes the completed correction.

## Operation names

| Previous form | Final form |
| --- | --- |
| `row.Value()`, `aggregate.Value()` | `expr.AsSelected(row)`, `expr.AsSelected(aggregate)` |
| `exemplar.Param(v)` | `expr.ParamOf(exemplar, v)` |
| `selected.Key()` | `expr.Key(selected)` |
| `aggregate.Filter(ps...)` | `expr.Filter(aggregate, ps...)` |
| `a.Eq(v)`, `a.Ne(v)`, `a.In(vs...)` in HAVING | `expr.EqValue(a, v)`, `expr.NeValue(a, v)`, `expr.InValue(a, vs...)` |
| `a.Lt(v)`, `a.Lte(v)`, `a.Gt(v)`, `a.Gte(v)` in HAVING | `expr.LtValue(a, v)`, `expr.LteValue(a, v)`, `expr.GtValue(a, v)`, `expr.GteValue(a, v)` |
| `a.IsNull()`, `a.IsNotNull()` in HAVING | `expr.IsNullValue(a)`, `expr.IsNotNullValue(a)` |
| `a.Asc()`, `a.Desc()` for selected/aggregate values | `expr.AscValue(a)`, `expr.DescValue(a)` |
| `having.Not()` | `expr.HavingNot(having)` |
| Selected text `.Like`, `.ILike`, `.Contains`, `.IContains`, `.StartsWith`, `.EndsWith`, `.IStartsWith`, `.IEndsWith` | `expr.TextLikeValue`, `expr.TextILikeValue`, `expr.TextContainsValue`, `expr.TextIContainsValue`, `expr.TextStartsWithValue`, `expr.TextEndsWithValue`, `expr.TextIStartsWithValue`, `expr.TextIEndsWithValue` |
| `datatable.Having(codec, aggregate)` | Wrap with `expr.ValueFilter`, `expr.OrderValueFilter`, or `expr.TextValueFilter` |
| `datatable.NullableHaving(codec, aggregate)` | Wrap with `expr.NullableValueFilter`, `expr.NullableOrderValueFilter`, or `expr.NullableTextValueFilter` |
| `Order[S]` / `SelectedOrder[S]` aliases | Exported non-alias structs; retain `.NullsFirst()` and `.NullsLast()` |

Row functions and `expr.Value(column)` / `expr.Param(column, v)` keep their names.
Selected comparison functions accept the same exact comparison value as the old
method, including present values for nullable operands. Typed sealed capabilities
retain source, value, ordering, nullability, and phase restrictions. There is no
reflection or type erasure in the new operation path.

## Hidden receiver methods

| Area | Before | After | Resolution |
| --- | ---: | ---: | --- |
| expr | 113 | 0 | 108 DSL methods become 24 functions; four ordering methods move to exported structs; one SQL scanner adapter moves to internal/ormcore |
| datatable | 8 | 0 | Six report methods become private functions; two standard io.Writer adapters move to datatable/internal/exportio |
| Other consumer-facing packages | 362 | 362 | Exact commented method exceptions remain for the lead |

Six exported filter adapters supply the retained datatable interface operations.
The [exception list](../../public_api_method_exceptions_test.go) identifies the
remaining receivers and exact methods, with reasons. No blanket package or type
exemptions were added.

## Order typing and footprint

`Order[S]` and `SelectedOrder[S]` now have distinct exported, non-alias definitions.
Their zero-sized scope fields retain `*S`; selected ordering retains its distinct
phase marker. Other D1 aliases still use their original pointer-scoped types.

The retained pre-migration and current Go export metadata both report **72-byte
size, 8-byte alignment, and two exported methods** for each ordering descriptor
on arm64. This measures the descriptor layout, not an isolated compiler or binary
footprint difference; no benchmark or footprint suite was run.

Current compiler metadata preserves the original rejections for
`aggregate_order_model`, `projection_order_scope`, and `window_order_owner` in
both fixtures: selected order cannot enter model ordering, and source scopes
cannot cross projection or window ordering.

## Guards and documentation

- `TestConsumerAPIMethodVisibility` rejects exported methods on private or alias
  receivers. The exact external exception list rejects stale entries as well.
- `TestConsumerAPIDocumentation` rejects new undocumented functions, types,
  methods, and exported interface method declarations. It rejects stale or
  duplicate backlog entries, and allows no DSL backlog exceptions.
- Every exported DSL function and type needs a standalone `Example:` line.
- `TestConsumerAPIDSLGoDocSurface` uses Go's documentation model to ensure every
  exported DSL function, type, and declared method is discoverable.
- `TestConsumerAPIInventory` writes the same parsed source inventory on request.

The scan includes generated and platform-specific declarations, and excludes
tests, nested modules, internal packages, testdata, vendored code, and main
packages. Source comments and generators own docs; generated output was refreshed
with `make generate`.

| Count | Before | After |
| --- | ---: | ---: |
| `database/orm/expr` undocumented declarations | 48 | 0 |
| `database/orm/expr` functions/types missing standalone examples | 441 | 0 |
| `datatable` undocumented declarations | 84 | 0 |
| `datatable` functions/types missing standalone examples | 77 | 0 |
| All undocumented declarations | 3,866 | 3,730 |
| External documentation backlog | 3,734 | 3,730 |

The fixed [backlog](../../testdata/public-api-undocumented.txt) contains 3,730
declarations outside the DSL. Four generated countries.Status methods became
documented through the enum emitter and were removed from the backlog.

## Compiler diagnostic updates

Both catalogues now pin the following meaningful mismatches. The explicit scope
argument in `aggregate_filter_owner` makes both incompatible model names visible
in its diagnostic. All archived `.go.txt` source and retirement hashes remain
unchanged. The 88 affected derived mapping rows were refreshed through
`tools/goway_catalogue.py`'s `rule` function.

| Fixture | Case | Old diagnostic fragment | New diagnostic fragment |
| --- | --- | --- | --- |
| consumer | `aggregate_filter_after_window` | `has no field or method Filter` | `A (type expr.Selected[models.Order, int64]) does not satisfy expr.FilterableAggregate[S, A]` |
| consumer | `aggregate_filter_boolean_order` | `has no field or method Gt` | `does not match inferred type expr.OrderedValueOperand[models.Order, bool]` |
| consumer | `having_boolean_range` | `has no field or method Gt` | `does not match inferred type expr.OrderedValueOperand[models.Order, bool]` |
| consumer | `aggregate_filter_having` | `expr.havingPredicate[*models.Order]) as expr.predicate[*models.Order] value` | `in call to expr.Filter, type expr.HavingPredicate[models.Order]` |
| consumer | `aggregate_filter_nullable` | `as decimal.Decimal value` | `in call to expr.GtValue, type value.Nullable[decimal.Decimal]` |
| consumer | `having_nullable` | `as decimal.Decimal value` | `in call to expr.GtValue, type value.Nullable[decimal.Decimal]` |
| consumer | `interval_sum_decimal` | `as temporal.Interval` | `does not match inferred type temporal.Interval for V` |
| consumer | `aggregate_filter_owner` | `expr.Predicate[models.User]) as expr.predicate[*models.Order] value` | `in call to expr.Filter[models.User], A (type expr.NullableOrderedAggregate[models.Order, decimal.Decimal])` |
| goway | `aggregate_filter_after_window` | `has no field or method Filter` | `A (type expr.Selected[models.Order, int64]) does not satisfy expr.FilterableAggregate[S, A]` |
| goway | `aggregate_filter_boolean_order` | `has no field or method Gt` | `does not match inferred type expr.OrderedValueOperand[models.Order, bool]` |
| goway | `having_boolean_range` | `has no field or method Gt` | `does not match inferred type expr.OrderedValueOperand[models.Order, bool]` |
| goway | `aggregate_filter_having` | `expr.havingPredicate[*models.Order]) as expr.predicate[*models.Order] value` | `in call to expr.Filter, type expr.HavingPredicate[models.Order]` |
| goway | `aggregate_filter_nullable` | `as decimal.Decimal value` | `in call to expr.GtValue, type value.Nullable[decimal.Decimal]` |
| goway | `having_nullable` | `as decimal.Decimal value` | `in call to expr.GtValue, type value.Nullable[decimal.Decimal]` |
| goway | `interval_sum_decimal` | `as temporal.Interval value` | `does not match inferred type temporal.Interval for V` |
| goway | `aggregate_filter_owner` | `expr.Predicate[models.User]) as expr.predicate[*models.Order] value` | `in call to expr.Filter[models.User], A (type expr.NullableOrderedAggregate[models.Order, decimal.Decimal])` |

Compiler metadata confirms the expected diagnostics for all **885 consumer**
and **920 goway** active negative packages. Three cases per fixture were rewritten
to fail for missing ordering or aggregate-phase capabilities rather than a removed
method. The compiler catalogue test harnesses were not run.

## Editor probes and guides

Updated `typed-aggregate-condition`, `typed-aggregate-filter`,
`selected-arithmetic-comparison`, `selected-expression-key`, and
`typed-filtered-comparison` to probe the free functions. Added
`typed-aggregate-condition-types`, `typed-aggregate-filter-types`,
`selected-arithmetic-types`, and `selected-expression-key-types` so concrete scope
and value types remain asserted. The transaction-value-lock prefix was refreshed
from its exact migrated expression. Both lead-fixed probe anchors were preserved.

All 487 probe source anchors resolve. The compiled source owns the migrated guide
excerpts; the excerpt refresh is idempotent. Historical before examples and actual
methods on exported rows/cursor/window/CASE/JSON-path types remain as applicable.

## Surface and verification

The original pinned-surface diff was inspected before refreshing. All orm and rows
sections were byte-identical. Existing expr sections changed only by the approved
method removals and ordering return types. Added sections describe eight sealed
capabilities and six exported adapters. The pinning list then added exactly 30
typed package function signatures; a second unrefreshed diff confirmed there were
no other changes. The final review also confirmed that selected null-test and
ordering capability inputs explicitly retain both source `S` and value `V`. The documented `FOUNDRY_UPDATE_METHOD_SURFACE=1` mechanism
refreshed the snapshot, and `TestPublicMethodSurface` passed.

`go vet ./...` passed in root, consumer, and goway. The new guards passed.
A regression source covers empty selected sources and zero-value group adapters;
it was not executed. Selected ordering and null-test capabilities explicitly retain
both `S` and `V`; their final signature-only correction was reviewed before a
second documented pin refresh. Existing row-package examples were also migrated.

No behavioral, real-gopls, benchmark, footprint, or compiler-catalogue test suite
was run. Those checks remain with the lead. No commits were created.

Open design questions: **none**. The 362 non-DSL visibility exceptions and 3,730
documentation backlog entries are explicitly deferred lanes, as authorized.

## Files

The [complete worktree file list](public-api-migration-files-20261031.txt) includes
inherited O+D2+C2 changes. This correction adds the operation/capability owner in
`database/orm/expr/selected_functions.go`, typed group adapters in
`database/orm/expr/value_filter.go`, the internal SQL scanner and export I/O
adapters, the root guard tests and backlog, and the reviewed `tools/exprmigrate`
codemod/documentation helpers. It migrates 3,140 typed call expressions across
546 existing source files, with explicit follow-up edits to negative cases and
editor fixtures. Framework guides, generator-owned docs/manifests, compiler
catalogues/mapping, the pinned surface, and CHANGELOG are updated.

## Retirement follow-up

The phase transition is `expr.AsSelected(e)`. The intermediate name reused a
retired spelling and was removed from framework code, fixtures, all active
testdata, guides, probes, compile-negative cases, the migration table, CHANGELOG,
and the migration tool's output and documentation catalogue. Historical retirement
evidence and the removed-name column in the migration table retain their spelling.

The audit compared all new exported package functions against every entry for
their package in `internal/retirement/denied.json`: 70 functions in `expr` and
four in `datatable`. The phase transition was the only collision. The complete
retirement tests also passed against all 114 ledger entries across 13 packages,
including public method exposure checks. No other collisions remain.

Before refreshing the method pin, the unrefreshed test produced exactly one
changed line: the phase-transition function's name. Its type parameters,
`SelectSource[S, V]` input, and `Selected[S, V]` result are unchanged. The snapshot
was refreshed through `FOUNDRY_UPDATE_METHOD_SURFACE=1` and checked against that
reviewed diff. No other API signature, field, or method changed in this follow-up.

The `typed-aggregate-filter` and `typed-filtered-comparison` probes now expect
`AsSelected` in completion. The `typed-transaction-value-lock` source prefix uses
the renamed call. All typing assertions and both lead-fixed anchors are retained.
The catalogue's own `rule` function refreshed 72 derived mapping rows, including
four consumer rows affected by the preceding testdata correction.

The authorized verification passed: `go vet ./...` in root, consumer, and goway;
`go test -count=1 ./internal/retirement/...`; `make docs-check`;
`TestPublicMethodSurface`; goway's `TestGuideSnippets`; and
`TestInvalidPublicContractsFailCompilation` in both fixtures. Diagnostic
expectations did not change. The earlier footprint measurements precede this
rename; footprint and real-gopls tests were not rerun for this follow-up.
No commits were created.
