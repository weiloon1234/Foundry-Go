package config

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// JSON declares a structured setting, such as a slice, map or settings struct.
// Its text representation is JSON in every source, including environment
// variables. File adapters normalize collections into that representation.
// Unknown struct fields and trailing JSON values are rejected. Custom decoding
// behavior, including null handling, follows the declared Go type's JSON codec.
func JSON[T, V any](name string, field func(*T) *V) Key[T, V] {
	return NewKey(name, field, func(raw string) (V, error) {
		var value V
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&value); err != nil {
			return value, err
		}
		var extra json.RawMessage
		if err := decoder.Decode(&extra); err != io.EOF {
			return value, fault.New(fault.Invalid, "structured setting requires one JSON value")
		}
		return value, nil
	})
}
