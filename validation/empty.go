package validation

import (
	"reflect"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/value"
)

type emptyKind string

const (
	emptyText       emptyKind = "text"
	emptyCollection emptyKind = "collection"
	emptyValue      emptyKind = "value"
)

type emptyCheck[T any] struct {
	kind       emptyKind
	serverOnly bool
	err        error
}

// Empty content is different from Go's zero value. Numeric zero, false and
// ordinary structs are supplied values; only text and collections can be empty.
// Optional/Nullable states are unwrapped by their typed rule adapters.
func nativeEmptyCheck[T any]() emptyCheck[T] {
	if value.IsOptionalType[T]() || value.IsNullableType[T]() {
		return emptyCheck[T]{err: invalid("empty-content rules require an unwrapped value; use the typed optional/nullable rule adapter")}
	}
	typ := reflect.TypeFor[T]()
	check := emptyCheck[T]{serverOnly: jsonshape.HasEncoder(typ, true) || jsonshape.HasDecoder(typ, true)}
	switch typ.Kind() {
	case reflect.String:
		check.kind = emptyText
		check.serverOnly = scalarWireTransform[T]()
	case reflect.Array, reflect.Slice, reflect.Map:
		check.kind = emptyCollection
		check.serverOnly = collectionTypeWireTransform(typ)
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Struct:
		check.kind = emptyValue
	default:
		check.err = invalid("empty-content rules require a concrete text, collection, numeric, boolean or struct value; validate pointers with Pointer/NotNil")
	}
	return check
}

func (c emptyCheck[T]) parameters() []Parameter { return []Parameter{parameter("empty_kind", c.kind)} }

func (c emptyCheck[T]) inspect(s *execution, input T) (empty, valid bool) {
	switch c.kind {
	case emptyText:
		text := reflect.ValueOf(input).String()
		if !textValue(s, text) {
			return false, false
		}
		return strings.TrimSpace(text) == "", true
	case emptyCollection:
		return reflect.ValueOf(input).Len() == 0, true
	default:
		return false, true
	}
}

// NonEmpty rejects blank text and empty collections without inferring wire
// presence. Zero numbers, false and ordinary structs pass. Domain validity is
// separate. Use Optional/Nullable/Pointer to unwrap those representations.
func NonEmpty[T any]() Rule[T] { return emptyContentRule[T](false) }

// Empty accepts blank text and empty collections. It does not treat numeric
// zero, false or a zero struct as null or omission. Text remains unchanged.
func Empty[T any]() Rule[T] { return emptyContentRule[T](true) }

func emptyContentRule[T any](wantEmpty bool) Rule[T] {
	check := nativeEmptyCheck[T]()
	if check.err != nil {
		return failed[T](check.err)
	}
	spec := Spec{ID: "foundry.non_empty", Parameters: check.parameters()}
	if wantEmpty {
		spec.ID = "foundry.empty"
	}
	return valueRule(spec, check.serverOnly, func(s *execution, input T) (bool, error) {
		empty, valid := check.inspect(s, input)
		return valid && empty == wantEmpty, nil
	})
}
