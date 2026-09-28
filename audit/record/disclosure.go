// Package record supplies typed audit snapshots and generated registration
// contracts below audit storage. Capturing a record performs no database I/O.
package record

import (
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Disclosure controls one generated model field's audit representation.
// Automatic includes ordinary fields and redacts sensitive codecs or conventional
// sensitive names.
// Exclude omits the entire field; Redact retains change flags without its value.
// There is deliberately no option to override sensitive-value/name protection.
type Disclosure uint8

const (
	Automatic Disclosure = iota
	Exclude
	Redact
)

func (d Disclosure) Validate() error {
	if d > Redact {
		return fault.New(fault.Invalid, "invalid audit field disclosure")
	}
	return nil
}

// SensitiveName is the shared rule for stored columns and nested JSON keys.
// It accepts snake_case, kebab-case and Go/JSON camel case. Explicit exclusions
// are still required for secrets whose names do not identify their meaning.
func SensitiveName(name string) bool {
	runes := []rune(name)
	var normalized strings.Builder
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			normalized.WriteByte('_')
			continue
		}
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) ||
			(i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsLower(runes[i+1]))) {
			normalized.WriteByte('_')
		}
		normalized.WriteRune(unicode.ToLower(r))
	}
	parts := strings.FieldsFunc(normalized.String(), func(r rune) bool { return r == '_' })
	for i, part := range parts {
		switch part {
		case "password", "passwd", "secret", "token", "credential", "credentials", "authorization", "apikey", "privatekey":
			return true
		case "api", "private":
			if i+1 < len(parts) && parts[i+1] == "key" {
				return true
			}
		}
	}
	return false
}
