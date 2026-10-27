package jsoncodec

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type fallbackValue struct {
	value   reflect.Value
	restore func() error
}

// A streaming method may decline its representation. An unnamed view removes
// only this receiver's methods; native JSON still owns its fields and tags.
func withoutMethods(value reflect.Value) (fallbackValue, error) {
	typ := value.Type()
	if typ.Kind() == reflect.Struct {
		return structFallback(value, make(map[reflect.Type]bool))
	}
	var bare reflect.Type
	switch typ.Kind() {
	case reflect.Bool:
		bare = reflect.TypeFor[bool]()
	case reflect.String:
		bare = reflect.TypeFor[string]()
	case reflect.Int:
		bare = reflect.TypeFor[int]()
	case reflect.Int8:
		bare = reflect.TypeFor[int8]()
	case reflect.Int16:
		bare = reflect.TypeFor[int16]()
	case reflect.Int32:
		bare = reflect.TypeFor[int32]()
	case reflect.Int64:
		bare = reflect.TypeFor[int64]()
	case reflect.Uint:
		bare = reflect.TypeFor[uint]()
	case reflect.Uint8:
		bare = reflect.TypeFor[uint8]()
	case reflect.Uint16:
		bare = reflect.TypeFor[uint16]()
	case reflect.Uint32:
		bare = reflect.TypeFor[uint32]()
	case reflect.Uint64:
		bare = reflect.TypeFor[uint64]()
	case reflect.Uintptr:
		bare = reflect.TypeFor[uintptr]()
	case reflect.Float32:
		bare = reflect.TypeFor[float32]()
	case reflect.Float64:
		bare = reflect.TypeFor[float64]()
	case reflect.Slice:
		bare = reflect.SliceOf(typ.Elem())
	case reflect.Array:
		bare = reflect.ArrayOf(typ.Len(), typ.Elem())
	case reflect.Map:
		bare = reflect.MapOf(typ.Key(), typ.Elem())
	default:
		return fallbackValue{}, safeerror.New("unsupported JSON fallback representation")
	}
	result := reflect.New(bare).Elem()
	result.Set(value.Convert(bare))
	return fallbackValue{result, func() error { value.Set(result.Convert(typ)); return nil }}, nil
}

func structFallback(value reflect.Value, ancestors map[reflect.Type]bool) (fallbackValue, error) {
	ancestors[value.Type()] = true
	defer delete(ancestors, value.Type())
	usedNames := make(map[string]bool, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		usedNames[value.Type().Field(i).Name] = true
	}
	fields := make([]reflect.StructField, 0, value.NumField())
	values := make([]reflect.Value, 0, value.NumField())
	restores := make([]func(reflect.Value) error, 0, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		original := value.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		underlying := field.Type
		if underlying.Kind() == reflect.Pointer {
			underlying = underlying.Elem()
		}
		if field.Anonymous && underlying.Kind() == reflect.Struct && name == "" {
			if ancestors[underlying] {
				continue
			}
			child, err := embeddedFallback(original, ancestors)
			if err != nil {
				return fallbackValue{}, err
			}
			// Explicit embedding avoids reflect.StructOf's promoted method set and
			// supports private embedded types without exposing their private fields.
			synthetic := "Embedded" + strconv.Itoa(i)
			for usedNames[synthetic] {
				synthetic += "_"
			}
			usedNames[synthetic] = true
			fields = append(fields, reflect.StructField{Name: synthetic, Type: child.value.Type(), Tag: `json:",embed"`})
			values = append(values, child.value)
			restores = append(restores, func(updated reflect.Value) error { child.value.Set(updated); return child.restore() })
			continue
		}
		if !field.IsExported() {
			continue
		}
		field.Anonymous = false
		fields = append(fields, field)
		values = append(values, original)
		restores = append(restores, func(updated reflect.Value) error { original.Set(updated); return nil })
	}
	result := reflect.New(reflect.StructOf(fields)).Elem()
	for i, item := range values {
		result.Field(i).Set(item)
	}
	return fallbackValue{result, func() error {
		for i, restore := range restores {
			if err := restore(result.Field(i)); err != nil {
				return err
			}
		}
		return nil
	}}, nil
}

func embeddedFallback(value reflect.Value, ancestors map[reflect.Type]bool) (fallbackValue, error) {
	if value.Kind() != reflect.Pointer {
		return structFallback(value, ancestors)
	}
	original := value
	working := value
	if value.IsNil() {
		working = reflect.New(value.Type().Elem())
	}
	child, err := structFallback(working.Elem(), ancestors)
	if err != nil {
		return fallbackValue{}, err
	}
	result := reflect.New(reflect.PointerTo(child.value.Type())).Elem()
	if !original.IsNil() {
		result.Set(child.value.Addr())
	}
	return fallbackValue{result, func() error {
		if result.IsNil() {
			if original.CanSet() {
				original.SetZero()
			}
			return nil
		}
		if original.IsNil() && !original.CanSet() {
			return safeerror.New("cannot populate private embedded JSON pointer")
		}
		child.value.Set(result.Elem())
		if err := child.restore(); err != nil {
			return err
		}
		if original.IsNil() {
			original.Set(working)
		}
		return nil
	}}, nil
}
