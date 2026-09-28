package contract

import (
	"encoding/json"
	"reflect"
)

// Primitive JSON declarations reuse the scalar schema source and ordinary JSON
// field validation. Named primitive values retain their qualified identity;
// custom serialization must still match the declared primitive representation.
func StringJSON[T ~string]() JSON[T] {
	return scalarFieldJSON(scalarKeyType[T](Type{Kind: StringKind}))
}
func BooleanJSON[T ~bool]() JSON[T] {
	return scalarFieldJSON(scalarKeyType[T](Type{Kind: BooleanKind}))
}
func IntegerJSON[T JSONKeyInteger]() JSON[T] {
	var zero T
	return scalarFieldJSON(scalarKeyType[T](Type{Kind: IntegerKind, Bits: uint8(reflect.TypeFor[T]().Bits()), Signed: !(^zero > zero)}))
}
func NumberJSON[T ~float32 | ~float64]() JSON[T] {
	return scalarFieldJSON(scalarKeyType[T](Type{Kind: NumberKind, Bits: uint8(reflect.TypeFor[T]().Bits())}))
}
func scalarFieldJSON[T any](scalar Scalar[T]) JSON[T] {
	if err := scalar.Validate(); err != nil {
		return JSON[T]{err: err}
	}
	return DefineJSONField[T](scalar.schema.snapshot())
}

// DynamicJSON is the explicit unrestricted JSON facility. It still enforces
// strict syntax, canonical value bounds and transport limits; no domain schema
// or concrete field shape is implied. Prefer generated DTO or primitive codecs.
func DynamicJSON() JSON[json.RawMessage] {
	const id TypeID = "encoding/json.RawMessage"
	return DefineJSONField[json.RawMessage](Schema{Root: id, Types: []Type{{ID: id, Kind: DynamicKind, Nullable: true}}})
}
