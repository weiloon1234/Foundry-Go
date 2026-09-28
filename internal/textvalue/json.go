// Package textvalue shares strict JSON string decoding for scalar value types.
package textvalue

import (
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Decode accepts a JSON string, never null, numbers, or composite values.
func Decode[T any](data []byte, parse func(string) (T, error)) (T, error) {
	var text *string
	if err := json.Unmarshal(data, &text); err != nil {
		return *new(T), fault.Wrap(fault.Invalid, "expected a JSON string", err)
	}
	if text == nil {
		return *new(T), fault.New(fault.Invalid, "non-nullable value cannot decode null")
	}
	return parse(*text)
}
