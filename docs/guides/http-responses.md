# Typed JSON response preparation

**Status: response encoding and descriptor composition are available.** Focused runtime, generator and independent consumer acceptance passed, including canonical consumer checks and actual-workspace completion, hover and definition. Full repository regression verification also passed, including real PostgreSQL, actual gopls, all generated-output freshness targets and documentation checks.

A generated JSON descriptor can prepare a response body while preserving the DTO
type. Encoding checks the same normalized schema used by decoding and future
client exporters. The framework returns bytes only after encoding and schema
validation succeed; a transport can then write its success headers and body.

```go
response := UserResponse{ID: userID, Email: email, State: activeState}
body, err := UserResponseJSON().Encode(ctx, response, limits)
```

`response` must have the descriptor's concrete DTO type. Passing another model's
response DTO is a compiler error. Map domain data into explicit response fields;
read the generated model field notices when choosing stored values or getters.
The encoder does not choose or apply model getters automatically.

## Slices and explicit null

Compose descriptors from an existing generated descriptor:

```go
users := contract.Slice(UserResponseJSON())
body, err := users.Encode(ctx, []UserResponse{response}, limits)

optionalUser := contract.Nullable(UserResponseJSON())
body, err = optionalUser.Encode(ctx, value.Of(response), limits)
body, err = optionalUser.Encode(ctx, value.Null[UserResponse](), limits)
```

These APIs return descriptors for ordinary `[]UserResponse` and
`value.Nullable[UserResponse]`. They do not introduce a collection class. Slice
nullability and element nullability are separate: `Slice(Nullable(...))` permits
null items. A nil Go slice represents JSON null; an allocated empty slice represents
`[]`. The descriptor's `Decode` method retains the same concrete result type.

Composition derives an owned schema graph from its element descriptor. It does
not require another schema declaration, and it preserves field, enum, model-ID
and nullability rules. Invalid element descriptors remain invalid.

## Bounds and failure behavior

Use the existing `contract.JSONLimits`. Bytes, Depth and Nodes bound the emitted
JSON document. Steps also bounds native values, indirections and fields inspected
before serialization; native inspection and schema validation each receive this
budget. The native inspection also limits string and byte content before codecs
run. Buffering has an explicit output cap, and encoding rejects invalid Unicode,
duplicate names and contract mismatches. Transport strings may contain escaped
NUL, while storage-oriented JSON snapshots keep their separate storage policy.

Field omission follows ordinary Go JSON tags. Custom `IsZero` and serialization
methods must be deterministic, safe for concurrent calls and bounded in their
internal work. The framework's input and output limits do not impose a hard
allocation bound inside arbitrary custom methods or their nested buffering.

`contract.EncodeError` has a safe public message and an owned `Issues()` slice.
Field issue paths use JSON Pointer; codec details remain available through the
internal error cause and are not included in that public message. Invalid
response values are internal failures. Declaration/configuration errors fail
before encoding begins.

Panic and `runtime.Goexit` in codecs become internal faults, and failures return
no body bytes. Cancellation waits for an active codec to actually finish rather
than releasing resources while it still runs. A codec panic remains visible even
if cancellation happens at the same time. Callers must keep input unchanged until
the operation returns.

`value.EncodeJSON` is the shared lower-level encoding boundary. It applies native
input inspection, strict standard-library encoding and wire bounds without
inferring a DTO schema. Normal typed HTTP response preparation uses the generated
descriptor so its declared field and value rules are also checked.

## Endpoint integration

[Typed endpoints](http-endpoints.md) own HTTP status/header handling and
[validation](validation.md); [pagination adapters](http-pagination.md) retain
page metadata while mapping models to declared response DTOs. Use those adapters
for ordinary handlers. OpenAPI and TypeScript export remain milestone 21 work.

UUID representation checks preserve the nil UUID, following `model.ID` serialization.
This is distinct from JSON null. Request validation and model binding decide
whether an empty identity is valid for a particular operation.
