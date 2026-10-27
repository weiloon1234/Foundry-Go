package value

import (
	"encoding/json/jsontext"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// List is a slice whose JSON form is never null: a nil or empty List encodes
// as [], and decoding null is rejected. Generated DTO schemas, OpenAPI and
// TypeScript describe it as a non-nullable array, so clients iterate it without
// null checks. It is an ordinary slice otherwise (len, range, append, index).
// Use a plain slice when null is a meaningful value.
type List[T any] []T

// MarshalJSON encodes a nil List as an empty array.
func (l List[T]) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	data, err := jsoncodec.Marshal([]T(l))
	return data, jsoncodec.MethodError(err)
}

// MarshalJSONTo is the encoding/json/v2 form used by Foundry's encoder. It
// keeps the caller's options, such as rejecting invalid UTF-8, for elements.
func (l List[T]) MarshalJSONTo(encoder *jsontext.Encoder) error {
	if l == nil {
		l = List[T]{}
	}
	return jsoncodec.MarshalEncode(encoder, []T(l))
}

// UnmarshalJSON rejects null and decodes elements with exact numbers.
func (l *List[T]) UnmarshalJSON(data []byte) error {
	if isNull(data) {
		return safeerror.NewFault(fault.Invalid, "list field cannot decode null")
	}
	decoded := []T{}
	if err := unmarshalPresent(data, &decoded); err != nil {
		return err
	}
	*l = decoded
	return nil
}
