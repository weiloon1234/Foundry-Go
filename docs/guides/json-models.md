# Typed JSON models and queries

<!-- compiled-examples -->

Store an ordinary payload as `value.JSON[Payload]`, with `orm.JSONOf` or
`orm.NullableJSONOf` in the table declaration. A handwritten `expr.JSONFieldOf`
binds a payload field pointer to its JSON tag; paths need no generated accessors.

## Model and value declarations

`value.NewJSON(payload)` snapshots a Go value. `value.ParseJSON[Payload](text)`
validates input. Both return errors. `Decode()` returns an independent payload;
mutating it does not change the snapshot. The zero `value.JSON[T]{}` is invalid.
Canonical documents sort object keys and normalize exact numeric spelling; arrays
retain order and duplicates. Decimal payload fields keep their string encoding.

The [document declarations](../../tests/fixtures/consumer/jsonqueries/tables.go)
bind payloads, SQL-nullable documents and typed JSON keys. Use column assignments
to update a snapshot; omission preserves the existing value, while a nullable
column's `Clear()` writes SQL NULL. Migrations must supply the physical JSONB type.

## Typed properties, arrays and maps

Declare reusable field bindings beside the columns:

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/jsonqueries/tables.go):

<!-- go-source: tests/fixtures/consumer/jsonqueries/tables.go -->
```go
var ProfileField = expr.JSONFieldOf(func(p *Preferences) *value.Optional[Profile] { return &p.Profile })

var NameField = expr.JSONFieldOf(func(p *Profile) *string { return &p.Name })

var AgeField = expr.JSONFieldOf(func(p *Profile) *int64 { return &p.Age })
```

`JSONFieldOf` validates the selected field and its JSON schema; call its `Validate`
at startup to report a bad binding there. `JSONProperty(parent, field)` follows
that binding, preserving the parent payload type. A foreign field cannot be used
on another payload. Optional, nullable, pointer and nested document layers use
`JSONOptional`, `JSONNullable`, `JSONPointer` and `JSONDocument` explicitly.

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/jsonqueries/path_sql_test.go):

<!-- go-source: tests/fixtures/consumer/jsonqueries/path_sql_test.go -->
```go
profile := expr.JSONOptional(expr.JSONProperty(expr.JSONRoot(expr.Value(f.Settings)), jsonqueries.ProfileField))
age := expr.OrderNullableRow(expr.JSONScalar(expr.JSONProperty(profile, jsonqueries.AgeField), codec.Signed[int64]()))
selected := rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(f.Query(), age.Eq(42))), age.Value()))
```

The linked test binds `f := jsonqueries.Documents`. `JSONRoot` starts a required
document path; `JSONNullableRoot` preserves a SQL-null ancestor. `JSONMapKey`
takes the map's exact key type; `JSONIndex` takes an `int32` slice index, including
negative indices from the end. Fixed arrays use `JSONFixedArrayOf` with
`JSONArrayElement`. Out-of-range indices and missing ancestors yield SQL NULL.
There is no untyped string-key step for a declared struct property.

`JSONScalar(path, codec)` returns `value.Nullable[P]`. `expr.OrderNullableRow(expr.JSONScalar(...))` and
`JSONTextScalar` retain supported ordered/text capabilities and accept present
comparison values. Exact named types and codecs survive extraction; enums do
not acquire LIKE through their string representation. Unsupported or opaque
custom shapes fail validation. Byte slices follow base64 JSON encoding, not
array indexing. Declared `,string` properties are unquoted before inspection.

## SQL NULL, JSON null and absent properties

| Operation | Meaning |
| --- | --- |
| `path.JSON()` | Nullable typed child snapshot; present JSON null requires a payload that can represent it |
| `expr.JSONTypeNullable(path.JSON())` | Nullable `database.JSONKind`; present `database.JSONNull` is distinct from SQL NULL |
| `path.JSON().IsNotNull()` | Includes present JSON null |
| `path.JSON().IsNull()` | Missing path or SQL-null ancestor |
| `expr.JSONTypeNullable(path.JSON()).Eq(value.Of(database.JSONNull))` | Present JSON null only |
| `expr.JSONLengthNullable(path.JSON())` | Array element count; NULL for missing, SQL NULL, JSON null or non-array |
| Scalar extraction | Missing, SQL NULL and JSON null all become SQL NULL |

Inspect the original nullable column to distinguish a SQL-null ancestor from a
missing descendant. `value.Optional[T]` with `json:",omitzero"` represents an
absent property. A nullable payload or pointer can represent JSON null; required
scalars cannot. JSON transport may encode distinct database states as `null`, so
use an explicit DTO representation if clients need that distinction.

## Whole documents and containment

Columns accept exact-payload `Eq`, `Ne`, `In`, `Contains` and `ContainedBy` values.
`expr.JSONType(expr.Value(column))` yields `database.JSONKind`, and `expr.JSONLength`
measures a whole array document. Kind constants in `database` are `database.JSONObject`, `database.JSONArray`,
`database.JSONString`, `database.JSONNumber`, `database.JSONBoolean` and `database.JSONNull`. Nullable and selected
function families preserve phase and one nullable layer. `database.JSONKind` has one
shared identity, including in retained report DTOs.

`JSONContains`/`JSONContainedBy` compare typed document operands. Capture a child
fragment with `value.NewJSON`, handle its error, and use
`expr.JSONContainsNullable(path.JSON(), path.JSON().Param(value.Of(fragment))).Eq(value.Of(true))`. SQL containment ignores array
order/duplicates, while whole-document equality preserves them. Optional payload
fields support partial containment documents. No raw SQL is needed.

## Composition and validation

Paths preserve aliases, CTEs, correlations and nullable join scopes. Use `rows.At`
for a JSON field of a derived rows schema, then start a path. Selected expressions
cannot become row paths. Extraction does not validate unselected siblings;
complete document decoding does. Invalid stored casts or payloads fail execution,
and collected reads publish no partial slice.

JSON codecs reject unknown/duplicate keys, missing required properties, invalid
Unicode, incompatible types and non-nullable nulls. Map keys must round-trip
canonically (`"01"` is not an integer key encoding). The `value.JSONMaxBytes`,
`JSONMaxDepth`, `JSONMaxNodes` and `JSONMaxNumberDigits` constants bound parsing
and canonicalization. Custom serializers own their internal work, shape and
concurrency behavior; they must preserve inputs and return independently owned
decoded values. Wire limits do not bound allocations inside those callbacks.

## Indexing JSON paths

Declared property names compile as escaped SQL literals for expression-index
matching. Map keys, indices and compared values remain parameters. The final
scalar property uses `->>` and its scalar cast; index that same expression.
Containment uses `@>` and can use an appropriate JSONB GIN index. Inspect actual
SQL and [plans](query-plans.md) before choosing an index.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).

### Startup validation

`JSONFieldOf` resolves the selector when declared, retaining any validation error.
Call each binding's `Validate()` before executor access; `orm.Check` only checks
table declarations, not unattached JSON fields. Go checks the payload and result
types, while startup validation checks pointer ownership, JSON tags and custom
encoding constraints. See the compiled [startup example](orm-migration.md#validate-json-property-declarations-at-startup).
