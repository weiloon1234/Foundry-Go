# Generic DTO contracts

The blocks in this section continue one scope: an `Inputs:` line names only what no earlier block in the section has introduced.

`UserDTO` denotes your concrete response struct, and `userJSON` is its `contract.JSON[UserDTO]` descriptor returned by the generated `UserDTOJSON()` helper.

The application owns `User` and any generated tables or identities used below.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `contract`, `http` as `foundryhttp`.

Declare a reusable Go envelope once. Its fields and JSON tags own the wire shape:

<!-- compiled-source: tests/fixtures/consumer/genericdto/envelope.go#generic-envelope -->
```go
//foundry:dto
type Envelope[T any] struct {
	Data  T      `json:"data"`
	Trace string `json:"trace"`
}
```

Inputs: `userJSON contract.JSON[UserDTO]`.

Generation emits `EnvelopeJSON[T](contract.JSON[T])`, returning
`contract.JSON[Envelope[T]]`. The function receives the application's concrete
`UserDTOJSON()` descriptor as `userJSON`:

<!-- compiled-source: tests/fixtures/consumer/genericdto/guide_recipes.go#generic-dtos-2 -->
```go
return foundryhttp.JSONResponse(200, EnvelopeJSON(userJSON))
```

The [independent consumer](../../tests/fixtures/consumer/genericdto/envelope.go)
connects this descriptor to its actual handler. It returns `Envelope[UserDTO]`,
retaining `UserDTO` fields, model-owned IDs, Optional/Nullable state and ordinary
Go autocomplete. `Envelope[ProjectDTO]` is a different handler/result type.
Generated OpenAPI and TypeScript describe the concrete registered instantiations.
Persistence models still require an explicit DTO transformation.

`EnvelopeValidationFields[UserDTO]()` returns typed fields for `Data` and `Trace`.
Compose existing rules with them; no validation rule runs merely because the
envelope is generic. Struct constraints, multiple/repeated parameters, nested
instantiations, collections and finite recursive references retain their types.
Only DTO annotations gain generic templates; model/config/transport declarations
keep their existing restrictions.

Pass one typed JSON argument per parameter, in declaration order. A parameter used
as a JSON map key additionally receives its existing `contract.JSONKey[K]` directly
after its JSON argument. For `Lookup[K comparable,V any]`, with a `map[K]V` field:

<!-- compiled-source: tests/fixtures/consumer/genericdto/guide_recipes.go#lookup-contract -->
```go
// Lookup keeps JSON key and value descriptors separate.
//
//foundry:dto
type Lookup[K comparable, V any] struct {
	Values map[K]V `json:"values"`
}
```

Build the descriptor by passing the key descriptor and the value descriptor:

<!-- compiled-source: tests/fixtures/consumer/genericdto/guide_recipes.go#generic-dtos-3 -->
```go
descriptor := LookupJSON(
	contract.StringJSON[string](), contract.StringJSONKey[string](), userJSON,
)
```

Key parsing and value parsing retain their existing owners; arbitrary comparable
types do not automatically become valid JSON object names. Even unused arguments
must be valid descriptors. Repeating the same Go type with conflicting wire
descriptions rejects assembly rather than choosing one schema silently.

Generic substitution happens during descriptor construction. Requests use the
existing compiled contract graph and bounded JSON codec. Byte-slice encoding,
nullable wrappers and custom value codecs retain native behavior. Native byte
slices, including named generic slices such as `Items[T] []T` specialized with
`byte`, use base64 and reject element restrictions that cannot be represented by
that wire contract. Custom byte encoders continue to produce typed arrays. Existing
`contract.Slice`, `contract.Nullable` and typed pagination remain the collection and
nullability APIs. There is no separate resource class or runtime model discovery.

Low-level `contract.DefineGenericJSON`, `JSONParameter`, `JSONSliceType`, `GoTypeID`
and `JSONArgumentID` are generator construction boundaries. Ordinary consumers use
their generated functions. As with `DefineJSONField`, handwritten metadata authors
own agreement with native Go serialization; these helpers do not infer an arbitrary
schema from sample data.

The independent fixture covers typed HTTP, nested schemas, compiler rejection,
strict TypeScript with real HTTP, and actual gopls completion. Generation is
deterministic.

Five native samples measured a median 12.7 µs for generic descriptor construction
versus 5.7 µs for the concrete envelope; construct and retain descriptors at
assembly, as shown above. Encoding measured about 6.4 µs and 105 allocations for
both. The complete in-process HTTP echo measured about 19.7/19.5 µs, with 247
allocations for both (including the test request/recorder). These fixture results
do not promise application-wide throughput.

Equivalent two-endpoint private consumers generated 7,796 bytes across three
generic files versus 11,252 bytes across four concrete files, with the same four
generated package imports. Cold/unchanged/edited generation took 4.52/0.48/0.73 s
for generic and 4.06/0.48/0.50 s for concrete; builds took 4.05/0.24/0.49 s and
4.29/0.25/0.48 s. These are single sequential local-checkout samples with isolated
Go build caches and an existing module cache, not release or statistical claims.
The linked evidence retains commands, samples, source hashes and measurement
harnesses.
