package querycore

import (
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"unicode/utf8"
)

func ValidateJSONKind(kind string) error { return database.JSONKind(kind).Validate() }
func ValidateJSONProperty(name string) error {
	if len(name) > value.JSONMaxBytes || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return safeerror.NewFault(fault.Invalid, "invalid JSON property name")
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
				err = safeerror.NewFault(fault.Invalid, "JSON map key must encode one property")
			}
			for k := range object {
				name = k
			}
		}
	}
	if err != nil {
		return "", safeerror.WrapFault(fault.Invalid, "invalid JSON map key", safeerror.Diagnostic("JSON map key failed", err))
	}
	return name, nil
}
