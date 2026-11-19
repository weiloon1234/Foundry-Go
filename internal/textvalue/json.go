// Package textvalue shares strict JSON string decoding for scalar value types.
package textvalue

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Decode accepts a JSON string, never null, numbers, or composite values.
func Decode[T any](data []byte, parse func(string) (T, error)) (T, error) {
	text, err := decodeString(data)
	if err != nil {
		return *new(T), err
	}
	return parse(text)
}

func decodeString(data []byte) (string, error) {
	var text *string
	if err := json.Unmarshal(data, &text); err != nil {
		return "", safeerror.WrapFault(fault.Invalid, "expected a JSON string", err)
	}
	if text == nil {
		return "", safeerror.NewFault(fault.Invalid, "non-nullable value cannot decode null")
	}
	return *text, nil
}
