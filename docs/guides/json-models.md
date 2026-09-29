# Typed JSON models and queries

Declare a JSON payload as an ordinary Go type and persist `value.JSON[Payload]`. Generation supplies concrete draft setters, predicates, scoped fields and projection decoders. The [independent consumer](../../tests/fixtures/consumer/jsonqueries/) exercises PostgreSQL writes, JSON queries, projections and eager relations.

## Model and value declarations

```go
type Preferences struct {
    Theme value.Optional[string] `json:"theme,omitzero"`
    Labels value.Optional[map[string]string] `json:"labels,omitzero"`
    Quota value.Optional[decimal.Decimal] `json:"quota,omitzero"`
}

//foundry:model table=accounts
type Account struct {
    ID model.ID[Account]
    Settings value.JSON[Preferences]
    Backup value.Nullable[value.JSON[Preferences]]
}
```

Use `value.NewJSON(payload)` to snapshot a Go value, or `value.ParseJSON[Preferences](text)` at an explicit input boundary. Both return errors. `.Decode()` returns a fresh payload, including independent maps and slices. Changing the input or a decoded result does not mutate the snapshot; construct a new value for an update. The unconstructed zero `value.JSON[T]{}` is invalid.

`JSON[T]` is comparable even when `T` contains maps or slices. Its canonical representation sorts object keys and normalizes exact numeric spelling. Array order and duplicate elements remain part of equality. Decimal payload fields retain [their existing string representation](database-codecs.md); integer fields retain their concrete range, and explicitly dynamic interface payloads decode numbers as `json.Number`. Float fields retain Go's approximate numeric semantics.

## Query and write examples

Using the compiling consumer declarations:

```go
filter, err := value.NewJSON(jsonqueries.Preferences{
    Theme: value.Set("dark"),
})
if err != nil {
    return err
}

f := jsonqueries.DocumentFields()
documents, err := jsonqueries.QueryJsonDocuments().
    Where(f.Settings.Contains(filter)).
    OrderBy(f.ID.Asc()).
    All(ctx, db)
```

`All` returns ordinary model slices. `First` retains the [optional model contract](model-queries.md). A different payload type, unrelated model predicate or text pattern passed to a JSON field fails compilation.

`Settings.Eq`, `Ne`, `In`, `Contains` and `ContainedBy` accept the field's exact `value.JSON[Preferences]` type. Optional payload fields let a containment document omit fields intentionally; required payload fields still require values. This first increment does not generate a separate partial-payload type. PostgreSQL containment ignores array order and repeated elements, whereas whole-document equality preserves them. [PostgreSQL JSON containment](https://www.postgresql.org/docs/18/datatype-json.html#JSON-CONTAINMENT)

`Settings.Kind()` returns a row expression with `query.JSONKind`: `JSONObject`, `JSONArray`, `JSONString`, `JSONNumber`, `JSONBoolean` or `JSONNull`. `IsJSONNull()` tests the JSON-null kind. Whole JSON fields expose counts, grouping and ordering, but no `Like`, `Sum`, `Avg`, `Min` or `Max` methods.

```go
updated, err := jsonqueries.QueryJsonDocuments().Update(
    ctx, db, id,
    jsonqueries.DocumentDraft{}.SetSettings(filter).ClearBackup(),
)
```

An omitted setter preserves the existing field during updates. Setting an invalid zero JSON snapshot fails before writing. Use an explicit migration with PostgreSQL `jsonb` columns; generation never changes the physical schema.

## Typed properties, arrays and maps

Generated JSON fields expose `Properties()` for ordinary structs and `At(...)` for arrays or maps. Property names come from Go fields and their JSON tags:

```go
f := jsonqueries.DocumentFields()
prefs := f.Settings.Properties()
documents, err := jsonqueries.QueryJsonDocuments().Where(
    prefs.Theme.Scalar().Like("%dark%"),
    prefs.Labels.At("lang").Scalar().Eq("en"),
    f.Tags.At(0).Scalar().Eq("go"),
).All(ctx, db)
```

Nested objects use another `Properties()` call. For example, `prefs.Profile.Properties().Next.Properties().Name.Scalar()` retains the same document scope through a recursive pointer. Map keys use the declared Go key type: `prefs.Profile.Properties().Attributes.At(2)` takes an `int`, whereas `Labels.At("lang")` takes a string. Array indices are `int32`; zero selects the first element and negative indices count from the end. An absent key, out-of-range index or missing ancestor yields SQL NULL. [PostgreSQL JSON extraction](https://www.postgresql.org/docs/18/functions-json.html)

Any path also offers `Length()`, the element count of a JSON array (SQL NULL when the path is missing or holds another kind, never an error), and `Contains(fragment)`, JSONB containment of a typed fragment at that path. `JSONField.Length()` measures a whole array document.

`Scalar()` returns a nullable expression with the declared scalar type, including named values and exact decimals. Text supplies `Like` and literal `Contains`; ordered values supply comparisons such as `Gt`. These convenience comparisons accept a present value while `.Value()` retains `value.Nullable[T]` in projections. Boolean, model-ID and enum scalar expressions use the ordinary row-expression comparison contract, such as `.Eq(value.Of(status))`.

`JSON()` selects the typed child snapshot, `Kind()` inspects its JSON kind, `Exists()` includes a present JSON null, and `IsMissing()` tests SQL NULL. `IsJSONNull()` tests a present JSON null. Scalar extraction combines missing values, SQL-null ancestors and JSON null into SQL NULL; use the path predicates or snapshot when the distinction matters. Inspect the original nullable field as well when SQL-null ancestors must be distinguished from a missing descendant.

Optional properties expose their present payload type. Nullable wrappers and pointers retain JSON-null capability in snapshots. Declared `,string` properties are decoded before path inspection: a quoted numeric property has its Go number type, and `JSON()`/`Kind()` observe that logical value. The full parent snapshot retains the parent's normal JSON encoding.

Properties use the same field/tag promotion rules as JSON value validation. Malformed JSON tag names fail generation and value construction. Custom JSON/text serializers own their wire shape and receive no inferred struct properties. Known scalar codecs remain available for framework enums, identifiers, decimals and temporal values; arbitrary custom serializers stay opaque. Byte slices use Go's base64 JSON encoding and do not receive array access.

Generated paths are row values. They retain aliases, CTEs, correlations and outer-join nullability through the existing scoped field sets. To query a projected JSON record, declare an alias/CTE and use that projection's `FieldsAt` helper, as described in [derived records](model-subqueries.md). Selected projection descriptors do not become row predicates implicitly.

Path filters and scalar SQL casts do not validate the entire stored document or unrelated siblings. `JSON()` validates the selected payload when decoding; a complete model read validates the entire document. Malformed external data can therefore fail snapshot decoding or produce PostgreSQL cast errors. Property names and indices are bound parameters, and invalid names/keys fail before query execution. Generated declaration constructors belong to framework integrations; ordinary applications use the generated fields.

## SQL NULL, JSON null and absent properties

| Declaration or operation | Meaning |
| --- | --- |
| `value.JSON[Preferences]` | Present JSON document with a concrete payload |
| `value.Nullable[value.JSON[Preferences]]` | Nullable SQL column containing a non-nullable JSON payload |
| `value.JSON[value.Nullable[string]]` | Present JSON document whose content can be JSON null |
| `value.Optional[T]` with `json:",omitzero"` | Payload property can be absent |
| `ClearBackup()` / `Backup.IsNull()` | Write/test SQL NULL |
| `State.IsJSONNull()` | Test JSON null |

Native pointers, maps, slices and interfaces can represent JSON null; non-nullable Go scalars and structs cannot. A present `Optional[T]` rejects null unless its inner contract explicitly permits it through `Nullable` or a JSON document. Ordinary `omitempty` and `omitzero` tags also make properties optional.

For a nullable SQL column, `.Kind().Value()` returns `value.Nullable[query.JSONKind]`. SQL NULL stays absent; a valid JSON-null payload has the present `JSONNull` kind. The SQL codec preserves this distinction. Ordinary JSON serialization can represent both as `null`, so consumers needing to transmit that distinction must declare an explicit DTO representation.

## Composition and validation

`JSONContains`, `JSONContainedBy` and `JSONType` accept typed row values. Their `Value` forms accept selected expressions while retaining the query phase. `Nullable` and `NullableValue` forms preserve SQL NULL; use `NullableRow`/`Nullable` to widen a non-null input explicitly. These operations share the existing AST, parameter binding, scope checking and grouping rules. They compose with complete projections and the existing query-source APIs.

`codec.JSON[T]()` validates stored payloads during hydration. Unknown or duplicate keys, missing required fields, incompatible values, invalid Unicode and non-nullable nulls fail. A failed unmarshal/scan preserves its destination. A collected model or projection read discards its results if any row fails decoding. Application validation cannot prevent external writers from storing a different shape, so hydration remains a runtime boundary.

Typed map keys must use their canonical Go encoding. Integer spellings such as `"01"`, `"+1"` and `"-0"` fail instead of silently collapsing into another key. Custom text-decoded keys must round-trip to the same property name. String-keyed maps preserve their exact keys.

The parser rejects invalid UTF-8, unpaired UTF-16 surrogates and NUL. It bounds input/canonical output to `value.JSONMaxBytes` (1 MiB), nesting to `JSONMaxDepth` (64), nodes to `JSONMaxNodes` (10,000), and numeric expansion to `JSONMaxNumberDigits` (the decimal package's 4,096-digit limit). Objects count keys as nodes. Ordinary Go input traversal and schema discovery have corresponding work bounds; wrapper/pointer traversal can consume depth before the encoded JSON does. Numeric expansion is checked cumulatively before constructing the final document.

Custom JSON or text methods own their internal schema, encoding work and concurrency behavior. They must preserve inputs and return independently owned decoded values. Their resulting representation still passes the wire limits; those limits do not bound memory allocated inside a custom callback. Foundry uses Go's codecs after strict shape checks, including exact property names and fixed-array lengths. Composite fields marked `,string` are rejected because Go otherwise ignores that option for composites. [Go JSON encoding contracts](https://pkg.go.dev/encoding/json)

Whole-document and generated path operations share the query AST, expression limits and SQL compiler. Query-time extraction does not mutate a document; construct a new typed snapshot and use the generated draft to update it.

## Indexing JSON paths

Declared property names are compiled as escaped SQL literals, while array indices, map keys and compared values stay bind parameters. A scalar directly below a property compiles to `->>`, so `prefs.Profile.Properties().Name.Scalar().Eq("Ada")` filters on `((settings -> 'profile') ->> 'name')`, and an expression index on exactly that path is usable even under the generic plans PostgreSQL chooses for cached prepared statements:

```sql
CREATE INDEX users_settings_profile_name ON users (((settings -> 'profile') ->> 'name'));
-- A non-text scalar compares through its cast; index the same expression:
CREATE INDEX users_settings_profile_age ON users ((CAST((settings -> 'profile') ->> 'age' AS bigint)));
```

Each ancestor adds one `->`; the final scalar uses `->>`. Whole-document and path containment (`Contains`) use `@>`, which a GIN index on the column serves (`CREATE INDEX ... USING gin (settings)` or `jsonb_path_ops`). Check a plan with [typed plan inspection](query-plans.md); [PostgreSQL JSON indexing](https://www.postgresql.org/docs/18/datatype-json.html#JSON-INDEXING) describes the operator classes.
