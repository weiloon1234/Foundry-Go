package validation

import (
	"encoding/json"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

// Native scalar operators cannot promise the same result on a custom wire
// representation. JSON numbers also have a special string-backed Go value.
func scalarWireTransform[T any]() bool {
	typ := reflect.TypeFor[T]()
	switch typ.Kind() {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64:
		return typ == reflect.TypeFor[json.Number]() || jsonshape.HasEncoder(typ, true) || jsonshape.HasDecoder(typ, true)
	default:
		return false
	}
}

func collectionWireTransform[S ~[]T, T any]() bool {
	return collectionTypeWireTransform(reflect.TypeFor[S]())
}

func collectionTypeWireTransform(typ reflect.Type) bool {
	return jsonshape.HasEncoder(typ, true) || jsonshape.HasDecoder(typ, true) || (typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 && !jsonshape.HasEncoder(typ.Elem(), true))
}

// scalarPrimitive never invokes a named scalar's JSON/text methods.
func scalarPrimitive[K Scalar](input K) any {
	value := reflect.ValueOf(input)
	switch value.Kind() {
	case reflect.String:
		return value.String()
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint()
	case reflect.Float32:
		return float32(value.Float())
	default:
		return value.Float()
	}
}

func scalarParameter[K Scalar](name string, input K) Parameter {
	return parameter(name, scalarPrimitive(input))
}

func scalarValue[K Scalar](s *execution, input K) bool {
	value := reflect.ValueOf(input)
	switch value.Kind() {
	case reflect.String:
		return textValue(s, value.String())
	case reflect.Float32, reflect.Float64:
		return finite(value.Float())
	default:
		return true
	}
}
