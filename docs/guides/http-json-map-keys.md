# Typed JSON object keys

**Status: focused runtime, generation, consumer, compiler and editor acceptance passed.**

Generated DTO contracts preserve the actual Go map key. Ordinary string and
integer keys, Foundry enums, model IDs and supported formatted values are
discovered from the field declaration:

```go
//foundry:dto
type InventoryResponse struct {
    ByNumber map[int16]string
    ByStatus map[models.Status]int
    ByUser   map[model.ID[models.User]]string
}
```

JSON object names remain strings. Integer names use canonical decimal spelling
at the declared width: `"32767"` fits int16; `"32768"`, `"01"`, `"+1"` and
`"-0"` are rejected. Enum keys must belong to the declared enum, and model IDs
retain their model owner and reject the nil UUID. The
[independent consumer](../../tests/fixtures/consumer/httpdto/map_keys.go) exercises
these types together.

Custom native text keys declare a value method returning
`contract.JSONKey[TheSameKeyType]`. The
[WarehouseKey example](../../tests/fixtures/consumer/httpdto/map_keys.go) keeps its
stored representation private and describes its string representation once:

```go
func (WarehouseKey) JSONKeyContract() contract.JSONKey[WarehouseKey] {
    return contract.DefineJSONKey(contract.DefineScalar[WarehouseKey](contract.Type{
        ID: "foundry.test/consumer/httpdto.WarehouseKey",
        Kind: contract.StringKind,
    }))
}
```

The generator discovers this method wherever that key is used; individual DTO
fields need no registration. Its native text methods remain responsible for
parsing and formatting. Keys must round-trip to exactly the original name, and
two names cannot decode to the same Go identity. The native map rules apply:
string keys bypass text encoding, text decoding takes precedence over integer
parsing, and JSON value methods do not supply legacy map-key codecs.

Key validation precedes DTO hydration. Failure returns a zero DTO and a `key`
issue at the containing map's path, excluding the rejected name. Response
encoding uses the same checks before returning publishable bytes. Native codec
panic or Goexit is an internal failure. Cancellation waits for a running codec
to finish; codec implementations must be bounded, deterministic, concurrency-safe
and must not retain borrowed input.

`JSONMapType[M]` binds the actual map key to its typed descriptor. A descriptor
for another key type fails compilation. Metadata includes the logical key type,
integer width, enum cases, format and model-ID requirement. Custom native rules
are marked `ServerOnly`; this does not claim that arbitrary Go behavior can be
reproduced by a TypeScript validator. Description snapshots own their metadata.

Use generated declarations in applications. The public descriptor constructors
are the explicit boundary for generation and custom native codec authors; they
do not inspect arbitrary Go business methods. Handwritten metadata must agree
with the native type's representation. Schema metadata alone cannot recreate
the private native key checker.

Combined transport full regression passed.
