package config

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"io"
	"strings"
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
		var data json.RawMessage
		if err := decoder.Decode(&data); err != nil {
			return value, err
		}
		if err := jsoncodec.Unmarshal(data, &value, jsonv2.RejectUnknownMembers(true)); err != nil {
			return value, err
		}
		if err := decoder.Decode(&data); err != io.EOF {
			return value, safeerror.NewFault(fault.Invalid, "structured setting requires one JSON value")
		}
		return value, nil
	})
}
