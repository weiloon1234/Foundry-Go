# Native custom JSON value contracts

**Status: focused runtime, generation, consumer, compiler and editor acceptance passed.**

Declare a value method named `JSONContract` alongside a custom value's native
Go serialization methods. It returns `contract.JSON[ThatSameValue]`. Generated
DTOs discover the method automatically, retain the concrete field type and
include the declared wire shape instead of inspecting private representation.

The [TrackingCode consumer](../../tests/fixtures/consumer/httpquery/json_contract.go)
shares one scalar declaration with its URL codec:

```go
func (TrackingCode) JSONContract() contract.JSON[TrackingCode] {
    return contract.ScalarJSON(TrackingScalar)
}
```

An ordinary `//foundry:dto` response can then use TrackingCode directly, in
slices or inside `Optional[Nullable[TrackingCode]]`. Keep `json:",omitzero"` on
Optional fields. The [response fixture](../../tests/fixtures/consumer/httpdto/custom_code.go)
requires no field-level codec registration. Its generated validation fields
retain TrackingCode as well.

For another representation, `DefineJSONValue[T]` accepts a complete owned schema
for T's actual native wire value. It requires T's exact qualified identity and
usable native methods. Normal response structs continue to use generated JSON
contracts. A custom codec author owns agreement between the Go type, native
methods and declared wire shape; runtime validation checks the input before
calling the decoder and checks the encoded output before HTTP publishes it.

Factories are called once per descriptor construction. They must be bounded,
deterministic and independent of receiver state. JSONType retains the concrete
factory return type. Invalid zero descriptors, root mismatches, factory panics
and Goexit reject construction. Shared nested definitions must agree after
normalization; conflicting definitions and authored duplicate IDs are rejected.
Description returns independent schema snapshots, and request handling does
not invoke the factory again.

Custom values require a value encoder and a supported native decoder on their
pointer. Both legacy JSON/text methods and [native streaming JSON value methods](http-streaming-json.md)
are supported. Pointer-only encoders are rejected because native map elements
and top-level values are not addressable. [Typed map-key contracts](http-json-map-keys.md)
have separate native method and identity rules. Arbitrary Go codec logic does
not imply an equivalent browser implementation, and persistence models do not
become public response DTOs through custom codecs.

## Named generic input values

Authentication request values now exercise custom contracts with named/scalar
generic arguments, such as `challenge.Token[Member, challenge.PasswordReset]`.
Generation uses canonical qualified names without spaces between generic arguments,
matching Go's runtime type name; nested named arguments and byte/rune aliases retain
their runtime identity. Anonymous composite identities keep their existing fallback.

An executable reports its package as `main`, including inside generic arguments.
The custom-contract inclusion boundary preserves the generated source identity
through a schema alias while keeping the factory graph intact. Only executable
namespace tokens may differ; model names, purpose names, generic structure and
other imported names must still match. DTO roots remain concrete non-generic
structs. New generator/runtime coverage is written for milestone 10 and unrun.
