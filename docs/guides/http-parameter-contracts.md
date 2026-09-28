# Typed path and query contracts

**Status: focused runtime, generation, consumer, compiler and editor acceptance passed.**

Path and query descriptions now include the scalar codec that actually performs
parsing and formatting. Inspect a path with `Parameters()`, a query with
`Parameters()`, or the complete endpoint with `Description()`.
`EndpointInfo.Path` preserves pattern order and catch-all semantics.
`EndpointInfo.Query` preserves required/optional and repeated-key cardinality.

Each described parameter has a `Scalar` containing an owned `contract.Type`
and `URLSyntax`. String, boolean, integer and float constructors attach their
metadata automatically. Native integer widths, signedness, float widths and
model-owned UUID identities remain explicit. The model-ID syntax rejects the
nil UUID; UUID syntax alone does not imply a database lookup or authorization.

Generated local and imported enums use `EnumPath`/`EnumQuery`, sharing their
typed enum descriptor with metadata and membership checks. Repeated enum query
parameters describe each scalar item, with cardinality retained separately.
Decimal, temporal and standard Go time values reuse the same format definitions
as generated JSON DTOs.

Custom protocols need an explicit value contract. The [consumer example](../../tests/fixtures/consumer/httpquery/scalar_metadata.go)
uses one typed declaration alongside its domain text methods:

```go
var TrackingScalar = contract.DefineScalar[TrackingCode](contract.Type{
    ID:   "foundry.test/consumer/httpquery.TrackingCode",
    Kind: contract.StringKind,
})

codec := foundryhttp.DescribeURL(
    foundryhttp.TextQuery[TrackingCode, *TrackingCode](),
    TrackingScalar,
)
```

The scalar descriptor and codec must use the same Go value type. Their private
type markers also reject converting one descriptor into another value's
descriptor. Invalid metadata or a nil codec rejects binding. A descriptor
validates and owns metadata; the custom codec author remains responsible for
describing its actual representation correctly. Its declared shape does not
promise equivalent browser parsing of arbitrary Go business logic.

An existing custom codec without metadata remains usable at the explicit codec
boundary and reports `Scalar == nil`. Exporters must identify that missing
contract instead of silently treating it as a string. `TextPath`/`TextQuery`
do not infer an arbitrary custom struct's format from its internal fields.
For handwritten enum bindings, use the enum constructors with the value's own
`EnumDescriptor()`; generated bindings select them automatically.

Descriptions copy nested enum bytes and metadata on every snapshot. Editing an
inspection result cannot change parsing, another request, a sibling parameter
or the next inspection result. Metadata inspection does not run Parse/Format.
Plain route name inspection and native HTTP handler interoperability remain
available; raw payload handlers do not acquire invented DTO schemas.

These declarations feed the shared manifest work in milestone 21. They do not
claim that OpenAPI or TypeScript generation is already complete.
