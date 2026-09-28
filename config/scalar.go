package config

import (
	"encoding"
	"math"
	"reflect"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// ScalarValue preserves named Go scalar types at the configuration boundary.
// Complex numbers, pointers and machine addresses are not configuration values.
type ScalarValue interface {
	~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}

// Scalar declares a scalar with its exact Go width. Reflection operates only on
// the decoded scalar; the compiler-checked accessor owns the destination field.
// No runtime field-name lookup is used. Floating values must be finite.
func Scalar[T any, V ScalarValue](name string, field func(*T) *V) Key[T, V] {
	key := NewKey(name, field, parseScalar[V])
	kind := reflect.TypeFor[V]().Kind()
	if kind == reflect.Float32 || kind == reflect.Float64 {
		key.check = func(value V) error {
			number := reflect.ValueOf(value).Float()
			if math.IsNaN(number) || math.IsInf(number, 0) {
				return fault.New(fault.Invalid, "configuration number must be finite")
			}
			return nil
		}
	}
	return key
}

func parseScalar[V ScalarValue](raw string) (V, error) {
	var result V
	value := reflect.ValueOf(&result).Elem()
	var err error
	switch value.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Bool:
		var parsed bool
		parsed, err = strconv.ParseBool(raw)
		value.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var parsed int64
		parsed, err = strconv.ParseInt(raw, 10, value.Type().Bits())
		value.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var parsed uint64
		parsed, err = strconv.ParseUint(raw, 10, value.Type().Bits())
		value.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		var parsed float64
		parsed, err = strconv.ParseFloat(raw, value.Type().Bits())
		if err == nil && (math.IsNaN(parsed) || math.IsInf(parsed, 0)) {
			err = fault.New(fault.Invalid, "configuration number must be finite")
		}
		value.SetFloat(parsed)
	}
	if err != nil {
		return *new(V), err
	}
	return result, nil
}

// Text declares a value with an encoding.TextUnmarshaler implementation. A fresh
// value is decoded on every load; decoder errors never return a partial value.
func Text[T, V any, P interface {
	*V
	encoding.TextUnmarshaler
}](name string, field func(*T) *V) Key[T, V] {
	return NewKey(name, field, func(raw string) (V, error) {
		var value V
		if err := P(&value).UnmarshalText([]byte(raw)); err != nil {
			return *new(V), err
		}
		return value, nil
	})
}

// Enum retains descriptor membership for file/environment values, defaults and
// typed overrides. A type-correct but undeclared Go enum value is still invalid.
func Enum[T any, V enum.Scalar](name string, field func(*T) *V, descriptor enum.Descriptor[V]) Key[T, V] {
	key := Scalar(name, field)
	key.declarationErr = descriptor.Validate()
	key.check = func(value V) error {
		if !descriptor.Contains(value) {
			return fault.New(fault.Invalid, "configuration enum value is not declared")
		}
		return nil
	}
	return key
}
