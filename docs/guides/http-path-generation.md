# Generated typed path bindings

The shared Foundry generator can bind route parameters from a handwritten Go
struct. The generated descriptor is ordinary Go code with concrete field types;
gopls can navigate it in the consumer workspace. The
[consumer declarations](../../tests/fixtures/consumer/httpkernel/path_declarations.go)
and [route tests](../../tests/fixtures/consumer/httpkernel/generated_routes_test.go)
show the complete usage.

```go
//foundry:path pattern=/users/{user}
type UserPath struct {
    User model.ID[models.User]
}

var ShowUser = foundryhttp.DefineRoute(
    foundryhttp.RouteSpec{
        ID: "users.show",
        Method: foundryhttp.GET,
        Access: foundryhttp.Public,
    },
    UserPathDescriptor(),
).Within(foundryhttp.DefineScope("/api", "api"))

location, err := ShowUser.URL(UserPath{User: user.ID})
```

Here `models.User` is the application's model, `model` is Foundry's model package,
and `foundryhttp` aliases Foundry's HTTP package. Use the existing
[generation workflow](model-generation.md). Generation emits
`user_path_foundry.gen.go`, including `UserPathDescriptor()`, and records ownership
in the same manifest used by model and enum generation. Handwritten route variables
and methods can refer to generated descriptors on a fresh checkout.

Registration uses `ShowUser.HandleRaw`, whose handler receives a concrete
`UserPath`. It cannot accept a path struct for another route. URL generation
cannot accept an ID belonging to a different model. ID parsing does not fetch a
model or establish authorization; those are separate boundaries.

## Field discovery and codecs

Use an exported, defined, non-generic struct. Field names default to snake case,
so `UserID` binds `{user_id}`. An explicit `path:"user"` tag binds another
parameter name; `path:"-"` excludes a field. Bound fields must be exported and
non-embedded. Every parameter needs exactly one field, and every bound field must
appear in the pattern. Path declarations do not accept persistence tags.

The field type selects the codec automatically:

| Field type | Generated codec |
| --- | --- |
| `model.ID[M]`, including aliases | `ModelIDPath[M]()` |
| Ordinary or named string | `StringPath[T]()` |
| Ordinary or named signed/unsigned integer | `IntegerPath[T]()` |
| Ordinary or named boolean | `BoolPath[T]()` |
| Ordinary or named `float32`/`float64` | `FloatPath[T]()` |
| Generated enum | `TextPath[T, *T]()` using generated membership validation |
| A complete text codec | `TextPath[T, *T]()` using its existing methods |

A text codec implements `MarshalText() ([]byte, error)` and
`UnmarshalText([]byte) error` on the pointer method set. Value marshal methods
are also supported. Exact decimals, temporal values, imported enums and generic
custom value types reuse their existing codecs. For scalar types with custom text
methods, those methods take precedence over the default scalar conversion. A
partial or incorrectly typed codec is a generation error.

Integers retain their concrete width and require canonical decimal input.
Booleans use exactly `true` or `false`. Generated enum methods reject undeclared
values both when parsing input and when generating URLs. Pointer, interface,
and other unsupported fields require an appropriate concrete value type with
a complete text codec. Native floats use `FloatPath[T]()` and preserve their
width; see [URL scalar behavior](http-url-scalars.md).

Custom methods must be deterministic, safe for concurrent calls and respect input
ownership. Failed text decoding returns no partially decoded value. Error causes
remain available to framework diagnostics through ordinary error inspection;
their text is not a public response message.

## Patterns and failure behavior

The generator and runtime share one path grammar. Parameters use `{name}`; a
final `{file...}` captures a subtree. An empty struct can describe a static path.
The current declaration directive uses whitespace-separated options, so its
pattern must not contain literal whitespace. Dynamic parameter values can contain
spaces and are encoded by URL generation.

The [routing guide](http-routing.md) defines exact trailing slashes, native method
matching, scopes, escaping, reserved slash-only values and route inspection.
Generated bindings use these same contracts. There is no second URL encoder or
router in the generator.

Invalid declarations fail before publication. The complete generated package is
type-checked before any output is replaced; stale checks do not write files.
Bindings are emitted in pattern order, independent of struct-field order.
Renaming a bound field or changing its type makes the generated output stale.

Generated path bindings cover route parameters. Request bodies, response DTOs,
query parameters and validation use the delivered [typed endpoint](http-endpoints.md)
contracts. Persistence
models do not become public response schemas automatically.
