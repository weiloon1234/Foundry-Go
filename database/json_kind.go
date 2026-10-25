package database

import "github.com/weiloon1234/Foundry-Go/fault"

// JSONKind identifies a JSON value's top-level shape. JSONNull is distinct from SQL NULL.
type JSONKind string

const (
	JSONObject  JSONKind = "object"
	JSONArray   JSONKind = "array"
	JSONString  JSONKind = "string"
	JSONNumber  JSONKind = "number"
	JSONBoolean JSONKind = "boolean"
	JSONNull    JSONKind = "null"
)

func (k JSONKind) Validate() error {
	switch k {
	case JSONObject, JSONArray, JSONString, JSONNumber, JSONBoolean, JSONNull:
		return nil
	}
	return fault.New(fault.Invalid, "invalid JSON kind")
}
