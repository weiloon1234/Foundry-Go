# T01 — Generic DTO schema generation

Prerequisites: accepted generator, contracts/SDK and consumer-startup foundation.
Status belongs to the [master](../00-master-architecture-and-parity.md#typed-api-delivery).

## Problem and consumer contract

Before T01, [discovery](../../internal/generate/discover.go) rejected a directly
annotated generic DTO, although concrete generic identities and pagination worked.
Applications should declare a reusable envelope once and receive a typed descriptor
for each concrete use without hand-maintaining properties in `contract.Schema`.

Implemented consumer shape:

```go
//foundry:dto
type Envelope[T any] struct {
    Data T      `json:"data"`
    Trace string `json:"trace"`
}

// Generated function: EnvelopeJSON[T](data contract.JSON[T])
// returns contract.JSON[Envelope[T]]. Fields/tags come from Envelope itself.
var response = http.JSONResponse(200, EnvelopeJSON(UserDTOJSON()))
```

An explicit typed argument descriptor supplies the otherwise unknown wire contract
for each type parameter. It does not let a string schema describe an integer T.
Descriptors must validate before registration; there is no per-request generic
schema discovery. Concrete DTO fields containing instantiated generic structs must
continue to work, with the same identities as standalone envelope use.

## Implementation ownership

Extend existing DTO discovery/emission and graph composition. Preserve declared
type parameters and constraints; scope the change to DTO declarations, without
silently permitting generic models, config, routes or unrelated directives.
Reuse identity normalization, custom scalar/JSON contracts, model rejection and
the existing graph's resource bounds. Root, nested, cross-package and executable
consumer identities must agree with runtime type identity.

Support multiple and repeated parameters, nested instantiations, slices, maps,
Optional/Nullable, constraints and finite recursive schemas. Reject unsupported
shape/constraint combinations with a diagnostic on the declaration. Do not replace
unknown T with `any`, stringify it or infer it from a sample value. Generic validation
field accessors must preserve field types wherever the existing DTO generator emits
them; validator registration remains an explicit consumer choice.

The manifest contains concrete instantiated schemas reachable from registrations.
OpenAPI components and TypeScript names distinguish `Envelope[UserDTO]` from
`Envelope[ProjectDTO]`, deterministically and without changing existing published
type IDs. TypeScript may emit concrete aliases; it need not reconstruct a Go generic
template to preserve accurate application types. Envelope fields are discovered
once from Go source, never separately specified in an exporter.

Existing `contract.Slice`, `contract.Nullable` and pagination responses remain the
canonical collection/null/page composition. Do not introduce another resource base
class or duplicate pagination envelope. Consumer transformations are ordinary typed
Go functions returning DTOs.

## T01 concrete source implementation

The implementation emits `EnvelopeJSON[T](contract.JSON[T])` and generic
validation field sets/functions. A parameter used as a map key additionally takes
`contract.JSONKey[K]` immediately after its JSON argument. The common schema compiler
resolves repeated concrete identities only when their normalized definitions agree.
The [consumer guide](../../docs/guides/generic-dtos.md) and
[public fixture](../../tests/fixtures/consumer/genericdto/envelope.go) show the API.
[Native acceptance evidence](../../docs/evidence/typed-api-t01.json) records the
full gate, focused races, compiler/TypeScript/gopls checks, fixes and cost results.

## Acceptance

- Independent consumer registers generic envelopes in actual typed endpoints;
  wrong handler output and wrong argument descriptors fail compilation.
- Generated/runtime identities agree for two concrete instantiations, nested
  multi-parameter values, aliases, imported packages and executable packages.
- JSON round trips include typed scalars, optional/null fields and recursive cases.
  Invalid shapes and embedded persistence models retain existing rejection behavior.
- OpenAPI and strict TypeScript preserve exact fields, model-owned IDs, wide-number
  representations and `exactOptionalPropertyTypes`; real HTTP output matches them.
- Generation/check/recovery are deterministic; unrelated output is unchanged.
- gopls completes envelope fields, typed arguments and returned DTO fields.
- Record descriptor assembly cost, request allocations, generated source size and
  incremental generation/build cost relative to a concrete handwritten envelope.

Complete the common milestone gate after the entire source/docs batch. Record
actual results and limitations in the master; T02 builds on the accepted graph.
