# Pilot round 2 ergonomics proposal

Source review at `34d87fb5`; planning only. Pilot references below are relative to `/Users/weiloon/Projects/ooo-orm-pilot`. No source changes or compile probes. Proposed tests are implementation acceptance requirements, not recorded passes.

## 2. Constants without an exemplar

The pilot borrows a Boolean expression for CASE parameters (`internal/datatables/reviews_datatable.go:94–96`). Shipped `expr.Param` requires a column (`database/orm/expr/value.go:59`); no public codec-based constant constructor exists.

Add in `database/orm/expr`:

```go
func Constant[S, V any](c codec.Codec[V], v V) RowExpression[S, V]
// expr.Constant[ReviewScope](codec.Bool[bool](), true)
```

Reuse `columnCodec` and `parameter` (`value.go:75–94`): validate the codec, clone and bind the value, retain it opaquely, and produce a reference-free parameter with scope `S`. “Constant” means a bound parameter, never interpolated SQL. Defer a separate `Bool[S]` alias; the general constructor removes the exemplar dependency without another spelling.

Exact codec/value and scope types remain checked; nullable values require their nullable codec. CASE retains the existing row-phase rules (`conditional.go:109`). Tests: CASE SQL/binding parity, invalid codec, clone isolation, nullable values, diagnostic redaction, wrong value/scope compile failures. Document in the expression/CASE migration examples. Footprint: one package function, existing carrier; measure the exercised expression probe, with no assumed zero cost.

## 3. Scoped expressions feeding Param

This is an input-contract mismatch, not merely inference: `Param` accepts `orm.ExpressionColumn`; `At` returns `Expression[S,V,P]` (`expression.go:45`). Existing `Select(At(...)).Param(v)` works (`value.go:53,73`).

Expose the existing row normalization (`value.go:46`) in `database/orm/expr`:

```go
func Row[S, V any](v RowValue[S, V]) RowExpression[S, V]
// expr.Row(expr.At(scope, column)).Param(v)
```

This is a typed adapter, not an unrestricted identity cast: an `Expression[S,V,P]` yields its projection result **P**, including nullability (`value.go:38–43`). It must preserve references, errors and codec. `Selected` does not implement `RowValue`, so this cannot demote aggregate/window/selected expressions into row predicates. Existing `Selected.Param` creates a new parameter rather than demoting its receiver. Leave column-based `Param` unchanged and do not widen capability bridges.

Tests: scoped SQL/binding parity, nullable result typing, wrong scope/value and selected-to-row negatives, zero-descriptor rejection. Document comparison-type versus projection-type parameters. Footprint: one package function forwarding to existing machinery, no new methods; measure expression-family impact.

## 4. Boolean declaration shortcuts

The repeated spelling is real (`internal/datatables/agents_datatable.go:42`, `reviews_datatable.go:58`). `rows.FieldOf` requires an explicit codec; neighboring constructors select one (`database/orm/rows/constructors.go:14–44`). ORM already infers Boolean codecs through `ColumnOf` (`database/orm/column.go:244`, `codecs.go:46`), so its need is naming consistency rather than capability.

Recommend the following thin pair, using existing carriers and `codec.Bool[V]()` (`database/codec/scalar.go:50`):

```go
// database/orm/rows
func BoolOf[R any, V ~bool](b Builder[R], ptr *V) Field[R, V]
// database/orm
func BoolOf[M any, V ~bool](b Builder[M], ptr *V,
    opts ...ColumnOption[V]) Column[M, V]
```

Reuse the existing bind/option paths. No Boolean carrier family, ordered/text operations, or nullable coercion; existing nullable constructors remain available. Mutated fields still use their Input carriers. Tests: built-in/named bool, false retained, codec options and mutation-role rejection, non-bool/foreign-owner negatives, projection contract parity. Add declaration-guide examples. Footprint: package functions only; rows is the demonstrated priority if the optional ORM spelling has measurable unrelated cost.

## 5. JSON property validation

`JSONFieldOf` already resolves and checks its selector when constructed, then exposes `Validate()` (`database/orm/expr/json_declaration.go:22–40`). `internal/ormcore/json_path.go:31` checks actual pointer ownership and JSON shape. The pilot declarations (`internal/datatables/invitations_datatable.go:129–137`) therefore need startup validation, not a new binding API.

Docs only: explicitly call each descriptor's `Validate()` before query execution. `orm.Check` accepts table declarations, not these descriptors (`database/orm/declaration.go:158–170`); do not imply table registration validates unattached JSON bindings. Go checks payload/result types and field existence, but cannot prove arbitrary selector behavior, JSON-tag ambiguity or custom encoding at compile time. No new registry or duplicate analyzer is warranted.

Extend the startup example in `docs/guides/orm-migration.md:962`; cover an invalid foreign/ignored selector failing before executor access, alongside existing owner/value negatives. No API or footprint change.

## 9. Framework enums in rows

The pilot's custom codec (`internal/datatables/countries_datatable.go:41`) is unnecessary at this base. `countries.Status` already implements `EnumDescriptor()` and exports `StatusCodec()` (`countries/status_foundry.gen.go:19,90`). It satisfies `rows.EnumOf`'s exact constraint (`database/orm/rows/constructors.go:44`):

```go
Status: rows.EnumOf(b, &r.Status)
```

Recommend docs only, with a consumer compilation example and valid/invalid enum hydration coverage. Prefer `EnumOf` because it also installs enum checks and disables aggregate-input capability; a custom `FieldOf` is not equivalent metadata. Preserve enum ownership and aggregate negatives. No new country-specific row constructor, codec, or library footprint. The pilot's contrary observation needs pin/example comparison before diagnosing a framework defect.

## 1. Pinning attachment slot stored names

Native slots reject persistence tags (`database/orm/relation.go:75`). The Go field names the binding; the explicit `name` argument to `attachments.ManyOf`/`OneOf` names the stored collection (`attachments/handle_many.go:29`, `attachments/slot.go:97–107`). Replace the pilot's retained-generated-name workaround (`internal/models/entity_model.go:19–21`) with:

```go
// Model: PublicMedia attachments.Many[Entity] // no foundry tag
// Table: PublicMedia: orm.SlotOf(b, &m.PublicMedia)
attachments.ManyOf(owner, "public-media", policy,
    extensions.BindSlot(Entities.Key(), Entities.PublicMedia))
```

Migration-guide entry: map old `foundry:"name=public-media"` to the explicit collection argument; retain the owner identity, StorageModel and collection name. Renaming the Go field alone must update its binding, not the stored name. Link `docs/guides/model-extension-slots.md:83`. Keep owner/key/slot typing and capabilities unchanged. Test/document renamed-field loading against the same collection, including loaded-empty versus missing. No API or footprint change.

## Recommendation summary

| Item | Recommendation | Effort | Risk |
| --- | --- | --- | --- |
| 2 | Codec-based `expr.Constant` | Small | Codec/nullability and redaction parity |
| 3 | `expr.Row` using existing normalization | Small | Projection-type and phase confusion |
| 4 | Thin `BoolOf` constructors | Small | Generic instantiation growth |
| 5 | Explain existing declaration/startup validation | Small | Unvalidated declarations at startup |
| 9 | Demonstrate existing `rows.EnumOf` | Small | Pilot pin mismatch; capability parity |
| 1 | Explicit stored collection-name mapping | Small | Persisted identity drift |

All additions are package functions; no Query/Table or carrier methods. Before implementation acceptance, measure existing and exercised probes: `q_with` must remain within its unchanged ceiling. No footprint numbers were measured here. Preserve every existing ceiling; any new probe uses `ceil(measured × 1.15)`. Record public-surface additions through the existing surface policy without broadening sealed capabilities.
