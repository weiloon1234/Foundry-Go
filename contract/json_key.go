package contract

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSONKeySyntax describes how string object names represent a declared key.
type JSONKeySyntax string

const (
	StringJSONKeySyntax  JSONKeySyntax = "string"
	IntegerJSONKeySyntax JSONKeySyntax = "integer"
	EnumJSONKeySyntax    JSONKeySyntax = "enum"
	ModelIDJSONKeySyntax JSONKeySyntax = "model_id"
	CustomJSONKeySyntax  JSONKeySyntax = "custom"
)

// JSONKeyInfo retains the key's logical scalar shape. Object member names are
// always strings; integer keys use canonical decimal spelling, preserving widths.
// ServerOnly identifies native code whose business rules cannot be exported as
// equivalent browser validation. NonZero applies to model identities.
type JSONKeyInfo struct {
	Value      Type          `json:"value"`
	Syntax     JSONKeySyntax `json:"syntax"`
	ServerOnly bool          `json:"server_only"`
	NonZero    bool          `json:"non_zero"`
}

// JSONKey ties map-key metadata and native codec behavior to exactly K.
// Its zero value is invalid. Construct it with the native helpers or DefineJSONKey.
type JSONKey[K comparable] struct {
	_       [0]*K
	runtime *jsonKeyRuntime
	err     error
}

type jsonKeyRuntime struct {
	typ    reflect.Type
	info   JSONKeyInfo
	scalar *compiledSchema
	check  func(context.Context, []string, value.JSONKeyLimits) error
}

// DefineJSONKey declares a custom native key's string or integer representation.
// Native JSON/text methods own parsing and formatting; their canonical round trip
// is checked before the containing DTO can be decoded. This declaration does not
// introduce a second serialization method or accept a custom untyped key parser.
func DefineJSONKey[K comparable](scalar Scalar[K]) JSONKey[K] {
	return defineJSONKey(scalar, CustomJSONKeySyntax, false)
}

func defineJSONKey[K comparable](scalar Scalar[K], syntax JSONKeySyntax, nonZero bool) JSONKey[K] {
	if err := scalar.Validate(); err != nil {
		return JSONKey[K]{err: err}
	}
	info, err := scalar.Description()
	if err != nil || (info.Kind != StringKind && info.Kind != IntegerKind) || !value.SupportsJSONKeys[K]() {
		return JSONKey[K]{err: invalidSchema()}
	}
	_, custom := jsonshape.MapKeyCapabilities(reflect.TypeFor[K]())
	return JSONKey[K]{runtime: &jsonKeyRuntime{
		typ: reflect.TypeFor[K](), info: JSONKeyInfo{Value: info, Syntax: syntax, ServerOnly: custom || syntax == CustomJSONKeySyntax, NonZero: nonZero},
		scalar: scalar.schema, check: value.CheckJSONKeys[K],
	}}
}

// Validate checks the immutable key declaration without invoking key methods.
func (d JSONKey[K]) Validate() error {
	if d.err != nil {
		return d.err
	}
	if d.runtime == nil {
		return invalidSchema()
	}
	return nil
}

// Description returns independently owned key metadata.
func (d JSONKey[K]) Description() (JSONKeyInfo, error) {
	if err := d.Validate(); err != nil {
		return JSONKeyInfo{}, err
	}
	return cloneJSONKeyInfo(d.runtime.info), nil
}

func cloneJSONKeyInfo(info JSONKeyInfo) JSONKeyInfo {
	// Scalar descriptors always have a single normalized node. Reuse schema
	// snapshot ownership rather than maintaining a second enum byte copier.
	schema := compiledSchema{description: Schema{Root: info.Value.ID, Types: []Type{info.Value}}}
	info.Value = schema.snapshot().Types[0]
	return info
}

func scalarKeyType[K comparable](wire Type) Scalar[K] {
	typ := reflect.TypeFor[K]()
	wire.ID = TypeID(typ.String())
	if typ.Name() != "" && typ.PkgPath() != "" {
		wire.ID = TypeID(typ.PkgPath() + "." + typ.Name())
	}
	return DefineScalar[K](wire)
}

// StringJSONKey preserves ordinary/named strings and native string-key precedence.
func StringJSONKey[K ~string]() JSONKey[K] {
	return defineJSONKey(scalarKeyType[K](Type{Kind: StringKind}), StringJSONKeySyntax, false)
}

// JSONKeyInteger is the native integer set accepted by JSON object keys.
type JSONKeyInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// IntegerJSONKey describes canonical decimal object names at the concrete width.
func IntegerJSONKey[K JSONKeyInteger]() JSONKey[K] {
	var zero K
	return defineJSONKey(scalarKeyType[K](Type{Kind: IntegerKind, Bits: uint8(reflect.TypeFor[K]().Bits()), Signed: !(^zero > zero)}), IntegerJSONKeySyntax, false)
}

// ModelIDJSONKey retains model ownership and rejects the nil UUID.
func ModelIDJSONKey[M any]() JSONKey[model.ID[M]] {
	return defineJSONKey(scalarKeyType[model.ID[M]](Type{Kind: StringKind, Format: UUIDFormat}), ModelIDJSONKeySyntax, true)
}
