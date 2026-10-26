# Generated JSON DTO contracts

**Status: DTO declaration and decoding APIs are available.** Full repository acceptance and focused runtime, generator and independent consumer
checks passed, including canonical generation and actual-workspace completion,
hover and definition. [Typed endpoints](http-endpoints.md) use these contracts
for request decoding, validation and response preparation.

Declare a public transport struct and run the existing Foundry generator. Its
generated descriptor retains the concrete DTO type and owns one normalized schema
for strict decoding and client contract export.

```go
//foundry:dto
type UpdateProfile struct {
    Email value.Optional[string] `json:"email,omitzero"`
    Nickname value.Optional[value.Nullable[string]] `json:"nickname,omitzero"`
}

var ProfileBody = UpdateProfileJSON()
```

`value` is Foundry's value package. Generation emits `UpdateProfileJSON()` in
`update_profile_foundry.gen.go`; handwritten code may refer to that function on a
fresh checkout. Changes to fields or tags make generated output stale. Generation
validates the whole package before replacing owned output. No separate schema
configuration is needed.

Use an exported, defined struct. [Annotated generic DTOs](generic-dtos.md) retain concrete type arguments in their generated contracts. Fields keep their Go names unless a
JSON tag declares another name. `json:"-"` excludes a field. The shared field
discovery applies Go's embedding rules and rejects ambiguous field promotion.
Unexported embedded pointers cannot be allocated during decoding and are rejected.

DTOs declared in an executable `main` package retain the source import path in
the generated schema. Runtime identity checks account for Go reporting that
package as `main`; ordinary imported DTOs require their exact qualified Go name.

## Omitted, null and present values

Property presence and nullability are separate contracts:

| Declaration | Omitted | JSON null | Present value |
| --- | --- | --- | --- |
| `string` | Rejected | Rejected | A string, including empty |
| `value.Optional[string]` with `omitzero` | Unset | Rejected | A set string |
| `value.Nullable[string]` | Rejected | Null | A present string |
| `value.Optional[value.Nullable[string]]` with `omitzero` | Unset | Explicit clear | Explicit replacement |

Ordinary `omitempty` and `omitzero` fields may be absent. Pointers, slices and maps
permit JSON null according to their declared Go representation. An Optional
wrapper does not silently turn every inner type into a nullable value; use
Nullable when a patch must distinguish omission from clearing.

To publish a list that is never null, declare the field as `value.List[T]`, an
ordinary slice type (`len`, `range`, `append` and indexing work unchanged):

```go
Tags value.List[string] `json:"tags"`
```

A nil list encodes as `[]`, decoding rejects `null`, and OpenAPI and TypeScript
describe the field as a non-nullable array, so clients iterate it without null
checks. Keep a plain slice where `null` is meaningful. `value.List[byte]` is
rejected because a byte slice's JSON form is base64 text.

A DTO that is only ever sent, never decoded from a request, can be declared with
`//foundry:dto role=response`. Its JSON codec and schema are unchanged; the
request validation field descriptors (`XValidationFields`) are not generated.

## Strict, typed decoding

The descriptor is immutable and can be reused concurrently. The lower-level
decoding API accepts an already bounded byte buffer and explicit resource limits:

```go
limits := contract.JSONLimits{
    Bytes: 4096,
    Depth: 16,
    Nodes: 500,
    Steps: 2000,
    Issues: 20,
}
request, err := ProfileBody.Decode(ctx, payload, limits)
```

`contract` is Foundry's contract package. `request` has type `UpdateProfile`.
Transport adapters own buffering, defaults and HTTP status mapping; this API does
not read an unbounded request body. [Typed endpoints](http-endpoints.md) supply
those HTTP transport defaults and limits.

Decoding rejects unknown or duplicate object names, case mismatches, missing
required fields, invalid Unicode, inappropriate nulls, out-of-range numbers,
invalid enum cases and unsupported scalar representations. It returns zero T on
every failure, rather than exposing a partially assigned DTO.

`contract.DecodeError` reports input failures with an owned `Issues()` slice.
Paths use JSON Pointer. Unknown fields produce one issue on their containing
object without echoing unknown names; received values are never included in the
error message. Internal causes remain available through ordinary error inspection.
Panic and Goexit in decoding codecs become safe internal faults. A direct
`Encode` call runs codecs on the caller's goroutine: a panic becomes a safe
internal fault, while `runtime.Goexit` ends that goroutine. Typed endpoints
encode their response inside the handler's isolated boundary, so Goexit there
is still a safe 500. Cancellation is checked
around bounded work, and an active codec is waited for until it actually returns.
A real codec panic is retained even if cancellation also occurs.

## Representations and schema ownership

Descriptors support ordinary and named scalars, model-specific IDs, local and
imported enums, exact decimals, temporal values, time.Time, fixed arrays, slices,
string-keyed maps and recursive DTO fields. Byte slices use base64. Supported
quoted scalar tags retain their JSON string representation. Custom codecs and
enums cannot be overridden with a quoted scalar tag.

Enum wire cases come from the existing typed enum descriptor. They are not copied
into a separately maintained HTTP list. Exact decimals use their string codec.
`json.Number` retains numeric text without floating-point conversion. Dynamic
`any` numbers use this exact representation even inside nested Optional/Nullable
fields; RawMessage retains its declared dynamic boundary.

`Description()` returns an independent normalized graph. Exporters consume this
same graph used by runtime field validation. Native integer widths are resolved
before export; floating-point values retain their concrete 32/64-bit bounds.
[OpenAPI and TypeScript exporters](client-contracts.md) consume this same normalized graph.

[Custom JSON values](http-custom-json.md) declare their native wire shape once;
[typed map keys](http-json-map-keys.md) preserve supported string, integer, enum,
model-ID and custom key contracts. Storage-oriented `value.JSON` fields and unannotated generic types are not inferred as public DTOs. Declare [generic DTO contracts](generic-dtos.md) explicitly and register concrete instantiations.
[Root slice/nullable descriptors and response encoding](http-responses.md) share
this schema and passed focused and full repository acceptance.

## Explicit response fields

Persistence models are not inferred as public DTOs. Direct model fields,
direct/transitive model embedding, and model types used as DTO map keys are
rejected on a fresh checkout and on regeneration alike. Map stored values or
explicit getters into a response struct that declares the fields to expose.

### Persistence model recognition and boundaries

A persistence model is a named type that appears as the model type argument of
an `orm.Table`, `orm.Builder`, `orm.Define` or `orm.TableOf` instantiation. These
are the declaration surface: `Define` is the only way to declare a table, and
its callback receives the `Builder` that every column and relation constructor
requires. Other `database/orm` generics, such as `Query` or `Predicate`, are not
treated as proof because raw fragments accept any type argument.

Recognition runs once per generation run, after every package's generated
overlay has passed its complete type check and before any output is published:

1. Every package in the run that imports `database/orm`, directly or
   transitively, contributes the types and generic instantiations recorded by
   its complete check. That includes function bodies, method bodies, package
   initializers, struct fields, signatures and type-parameter constraints, so
   `var H = Holder[Account]{}`, `func init() { _ = Holder[Account]{} }`, a
   non-erasing wrapper such as `func Table[M any](string) orm.Table[M, int]`,
   a generic alias of `orm.Table` and a named table field beside an embedded
   handle are all recognized.
2. Dependencies loaded from compiled export data, outside the run, that import
   `database/orm` contribute their package-level declarations: variable,
   function and method signatures, type definitions and constants.
3. One structural walk covers pointers, slices, arrays, maps, channels, struct
   fields, interfaces, signatures, constraints, unions and aliases. A generic
   instance is walked with its substituted fields and methods, so a handle
   written with a type parameter is recognized where the generic is
   instantiated. A generic model is recognized for every instantiation.
4. A generic function, type, alias or method is a wrapper when any mention of
   one of its type parameters is the model argument of a persistence generic
   or of another wrapper, in a body, signature, field or constraint,
   regardless of control flow. Its instances need not expose a handle.
   Examples are `func Register[M any]() { orm.Define[M](...) }` and a method
   `func (h Holder[M]) Init()` that declares a table for `M`. Every explicit or
   inferred instantiation of a wrapper in the run records the type argument at
   that parameter's position as a model. `Register[Account]()` and
   `Holder[Account]{}` both recognize `Account`; other type parameters are not
   marked. Wrappers of wrappers, including across packages of the run, are
   resolved to a fixpoint.

Every DTO, union and JSON multipart graph is then checked against the run's
complete model set. The result does not depend on which packages finished
first: a model declared by a sibling package, for example `tables` calling
`orm.Define[models.Account]` while `http` imports only `models`, is rejected in
every full run.

When discovery has already failed for a DTO package, for example on a struct
map key or on a model field whose type DTOs cannot encode, the reported
diagnostic names the persistence model when that package or its imports
declare the model. The original diagnostic follows it as
`(discovery also failed: …)`. A model declared only by an unrelated sibling
package keeps the original discovery diagnostic; the declaration still fails.

**Recognition limits:**
- **Scope of a run:** a `--dir` run recognizes models declared in the packages
  it loads, which are its selected packages and their import closure. A model
  declared only in a package outside that closure, such as a sibling `tables`
  package of a non-recursive run, is not visible to it. The full `make generate`
  and `make generate-check` runs over each generation root are the
  authoritative gate CI enforces.
- **Export-data initializers and bodies:** compiled export data has no
  initializer expressions or function bodies. A table that a package outside
  the run declares only inside a function, inside a generic wrapper, or behind
  an interface type such as `any` does not expose its model. Declare such
  tables with an explicit `orm.Table[M, K]` type, or include the package in
  the run.
- **Unexported export-data declarations:** export data omits a package's
  unexported package-level declarations unless its exported API or inlinable
  bodies reference them. An unexported table variable in a package outside the
  run is therefore not seen.
- **Named models only:** a model type argument that is not a named type, such
  as `orm.Table[*Account, int]` or an anonymous struct, fails generation with a
  positioned diagnostic. A wrapper instantiated with a non-named composite
  argument, such as `Register[*Account]()` or `Register[[]Account]()`, fails
  the same way. A wrapper instantiated with a basic or interface argument, such
  as `int` or `any`, records nothing. Function-local model types are not
  tracked because no DTO declaration can name them.
- **Wrappers outside the run:** wrapper resolution covers only packages in the
  run. A framework or export-data generic that calls `orm.Define[M]` only in
  its body is invisible unless its signature or members expose
  `orm.Table[M, K]` or `orm.Builder[M]`.
- **Fail-closed traversal:** every standard `go/types` type kind is handled
  explicitly. Any other kind fails generation with the position, package and
  object being inspected, rather than being skipped.
- **Custom codecs:** a type with an explicit JSON contract owns its wire shape,
  so its Go fields are not inspected for models.

Go's compiler retains descriptor owners, decoded result types, model-ID owners,
enum values and Optional/Nullable types. Runtime input validation complements
these checks; a valid UUID string does not prove model existence or authorization.

[Typed response preparation](http-responses.md) uses the same descriptor to
encode a body and validate it before transport headers or bytes are written.
