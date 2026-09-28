// Package contract owns typed transport descriptions shared by runtime adapters
// and client exporters. Persistence models do not implicitly become public DTOs.
package contract

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TypeID identifies a declaration or derived wire shape within a schema graph.
// Generated identities use qualified Go names; exporters own target-language
// naming and must not mistake these identifiers for OpenAPI component names.
type TypeID string

// Kind selects one wire-shape operation. References are explicit graph edges,
// so recursive DTOs do not require cyclic Go pointers in a schema declaration.
type Kind string

const (
	BooleanKind Kind = "boolean"
	StringKind  Kind = "string"
	IntegerKind Kind = "integer"
	NumberKind  Kind = "number"
	ObjectKind  Kind = "object"
	ArrayKind   Kind = "array"
	MapKind     Kind = "map"
	AliasKind   Kind = "alias"
	QuotedKind  Kind = "quoted"
	DynamicKind Kind = "dynamic"
	UnionKind   Kind = "union"
)

// Format is a supported scalar representation whose parser belongs to the
// corresponding framework value type. Empty format means an ordinary string.
type Format string

const (
	UUIDFormat          Format = jsonshape.UUIDFormat
	DecimalFormat       Format = jsonshape.DecimalFormat
	DateFormat          Format = jsonshape.DateFormat
	TimeFormat          Format = jsonshape.TimeFormat
	DateTimeFormat      Format = jsonshape.DateTimeFormat
	LocalDateTimeFormat Format = jsonshape.LocalDateTimeFormat
	IntervalFormat      Format = jsonshape.IntervalFormat
	Base64Format        Format = jsonshape.Base64Format
)

// Property binds an exact, case-sensitive JSON name to its declared wire type.
// Required describes presence independently of the target type's nullability.
type Property struct {
	Name     string `json:"name"`
	Type     TypeID `json:"type"`
	Required bool   `json:"required"`
}

// Variant binds one closed discriminator literal to an ordinary object payload.
// Payload properties come from that object's existing schema.
type Variant struct {
	Tag  string `json:"tag"`
	Type TypeID `json:"type"`
}

// Type is one normalized wire shape. Only fields appropriate for Kind may be
// set. Element describes array/map elements or an alias/quoted target. Integer
// Integer Bits==0 is normalized to the Go target's native int width. Number
// Bits==0 preserves json.Number lexemes; 32/64 describe concrete floating point.
// Length, when set, describes an exact array length, including an empty array.
// Cases are exact JSON string/integer constants, never floating-point metadata.
type Type struct {
	ID            TypeID              `json:"id"`
	Kind          Kind                `json:"kind"`
	Nullable      bool                `json:"nullable"`
	Discriminator string              `json:"discriminator,omitempty"`
	Variants      []Variant           `json:"variants,omitempty"`
	Properties    []Property          `json:"properties,omitempty"`
	Key           *JSONKeyInfo        `json:"key,omitempty"`
	Element       TypeID              `json:"element,omitempty"`
	Length        value.Optional[int] `json:"length,omitzero"`
	Bits          uint8               `json:"bits,omitempty"`
	Signed        bool                `json:"signed,omitempty"`
	Format        Format              `json:"format,omitempty"`
	Cases         []json.RawMessage   `json:"cases,omitempty"`
	byteElement   TypeID              // construction-only check: base64 must not discard element rules
	enumCases     func() ([]json.RawMessage, error)
	jsonContract  func() (Schema, error)
	jsonKey       *jsonKeyRuntime
}

// Schema is the normalized description at a serialization boundary. Construct
// typed JSON descriptors through generated code; definitions are validated and
// copied before use. Description returns another owned snapshot.
type Schema struct {
	Root  TypeID `json:"root"`
	Types []Type `json:"types"`
}
