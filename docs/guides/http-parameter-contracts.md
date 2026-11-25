# Typed path and query contracts

**Status: focused runtime, generation, consumer, compiler and editor acceptance passed.**

Path and query descriptions now include the scalar codec that actually performs
parsing and formatting. Inspect a path with `PathParameters(path)`, a query with
`QueryParameters(query)`, or the complete endpoint with `InspectEndpoint(endpoint)`.
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
accepts the scalar declared with the [native TrackingCode methods](http-custom-json.md):

The text codec and scalar metadata share this declaration. Use your application package path for the scalar identity.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `contract`, `fault`, `http` as `foundryhttp`.

<!-- compiled-source: tests/fixtures/consumer/httpquery/scalar_metadata.go#tracking-scalar -->
```go
// TrackingCode is an application-owned text protocol. The scalar declaration
// describes its wire value; the methods own its domain representation checks.
type TrackingCode string

func (v TrackingCode) MarshalText() ([]byte, error) {
	if !strings.HasPrefix(string(v), "track_") {
		return nil, fault.New(fault.Invalid, "invalid tracking code")
	}
	return []byte(v), nil
}
func (v *TrackingCode) UnmarshalText(data []byte) error {
	candidate := TrackingCode(data)
	if _, err := candidate.MarshalText(); err != nil {
		return err
	}
	*v = candidate
	return nil
}

var trackingType = reflect.TypeFor[TrackingCode]()

var TrackingScalar = contract.DefineScalar[TrackingCode](contract.Type{
	ID: contract.TypeID(trackingType.PkgPath() + "." + trackingType.Name()), Kind: contract.StringKind,
})
```

These recipes need no application model; the transport and helper types they use are declared or described on this page.

Inputs: `TrackingScalar contract.Scalar[TrackingCode]`.

<!-- compiled-source: tests/fixtures/consumer/httpquery/codec_recipe.go#tracking-url-codec -->
```go
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

These declarations feed the shared manifest and [OpenAPI and TypeScript exporters](client-contracts.md). Runtime decoding and exports share the same registered scalar contracts.
