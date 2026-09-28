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

Use an exported, defined, non-generic struct. Fields keep their Go names unless a
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
Panic and Goexit in codecs become safe internal faults. Cancellation is checked
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
OpenAPI and TypeScript output adapters remain milestone 21 work.

[Custom JSON values](http-custom-json.md) declare their native wire shape once;
[typed map keys](http-json-map-keys.md) preserve supported string, integer, enum,
model-ID and custom key contracts. Storage-oriented `value.JSON` fields and
arbitrary generic root declarations are not inferred as public DTOs.
[Root slice/nullable descriptors and response encoding](http-responses.md) share
this schema and passed focused and full repository acceptance.

## Explicit response fields

Persistence models are not inferred as public DTOs. Direct model fields and
direct/transitive model embedding are rejected, including before generated model
methods exist on a fresh checkout. Map stored values or explicit getters into a
response struct that declares the fields to expose. The model's generated field
notices remain the guide for choosing a stored field versus its getter.

Go's compiler retains descriptor owners, decoded result types, model-ID owners,
enum values and Optional/Nullable types. Runtime input validation complements
these checks; a valid UUID string does not prove model existence or authorization.

[Typed response preparation](http-responses.md) uses the same descriptor to
encode a body and validate it before transport headers or bytes are written.
