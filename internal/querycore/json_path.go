package querycore

import (
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"unicode/utf8"
)

func ValidateJSONKind(kind string) error {
	switch kind {
	case "object", "array", "string", "number", "boolean", "null":
		return nil
	}
	return fault.New(fault.Invalid, "invalid JSON kind")
}
func ValidateJSONProperty(name string) error {
	if len(name) > value.JSONMaxBytes || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return fault.New(fault.Invalid, "invalid JSON property name")
	}
	return nil
}

// JSONMapKey uses the JSON payload boundary's canonical typed map-key codec.
func JSONMapKey[K comparable](key K) (string, error) {
	encoded, err := value.NewJSON(map[K]bool{key: true})
	var name string
	if err == nil {
		var text string
		text, err = encoded.Text()
		if err == nil {
			var object map[string]json.RawMessage
			err = json.Unmarshal([]byte(text), &object)
			if err == nil && len(object) != 1 {
				err = fault.New(fault.Invalid, "JSON map key must encode one property")
			}
			for k := range object {
				name = k
			}
		}
	}
	if err != nil {
		return "", fault.Wrap(fault.Invalid, "invalid JSON map key", safeerror.Diagnostic("JSON map key failed", err))
	}
	return name, nil
}
