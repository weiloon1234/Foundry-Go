package contract

import (
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
)

// Slice declares an ordinary Go slice of the concrete DTO described by element.
// It derives its schema from that descriptor, including every element rule.
// JSON null decodes to a nil slice; [] decodes to an allocated empty slice.
// Native byte slices use base64 text; custom byte-value encoders retain arrays.
// Element nullability is independent: use Slice(Nullable(element)) when each
// item may itself be null. An invalid element produces an invalid descriptor.
func Slice[T any](element JSON[T]) JSON[[]T] {
	result := composeJSON[[]T](element.schema, element.Validate(), "slice:", ArrayKind)
	result.sourceName = "[]" + element.GoTypeName()
	return result
}

// Nullable declares an explicit null or a present value of the concrete DTO
// described by element. It reuses value.Nullable rather than introducing a
// transport-specific wrapper. The resulting descriptor can be composed with
// Slice, and Decode retains the same limits and all-or-zero failure contract.
func Nullable[T any](element JSON[T]) JSON[value.Nullable[T]] {
	result := composeJSON[value.Nullable[T]](element.schema, element.Validate(), "nullable:", AliasKind)
	result.sourceName = "github.com/weiloon1234/Foundry-Go/value.Nullable[" + element.GoTypeName() + "]"
	return result
}

func composeJSON[T any](base *compiledSchema, err error, prefix string, kind Kind) JSON[T] {
	if err != nil {
		return JSON[T]{err: err}
	}
	description := base.snapshot()
	root := TypeID(prefix + string(description.Root))
	node := Type{ID: root, Kind: kind, Element: description.Root, Nullable: true}
	if kind == ArrayKind {
		node = jsonSliceType(root, description.Root, reflect.TypeFor[T]().Elem())
	}
	description.Types = append(description.Types, node)
	description.Root = root
	schema, err := compileSchema(description)
	return JSON[T]{schema: schema, err: err}
}
