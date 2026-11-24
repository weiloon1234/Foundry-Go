package modelmeta

import (
	"fmt"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// ModelTag is the shared field declaration parsed before type-specific validation.
// StorageTag retains only the options that describe a persisted column.
type ModelTag struct {
	Column                                      string
	DatabaseDefault                             bool
	Role, Input, Mutate, Codec, Member, KeyRole string
	Slot, Ignore                                bool
}

// StorageTag returns the existing column/default grammar without generator options.
func (t ModelTag) StorageTag() string {
	if t.Ignore {
		return "-"
	}
	var parts []string
	if t.Column != "" {
		parts = append(parts, "column="+t.Column)
	}
	if t.DatabaseDefault {
		parts = append(parts, "default=database")
	}
	return strings.Join(parts, ",")
}

// ParseModelTag validates the complete struct tag, including misspelled namespaces.
// Field type and table-option compatibility are checked by the declaration owner.
func ParseModelTag(raw string) (ModelTag, error) {
	var result ModelTag
	tags, err := ParseTags(raw)
	if err != nil {
		return result, err
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if key != "foundry" && NearFoundry(key) {
			return result, safeerror.New(fmt.Sprintf("unknown struct tag %s; use foundry", key))
		}
	}
	text, exists := tags["foundry"]
	if !exists {
		return result, nil
	}
	if text == "" {
		return result, safeerror.New("foundry tag cannot be empty")
	}
	if text == "-" {
		result.Ignore = true
		return result, nil
	}
	seen := make(map[string]bool)
	parts, err := modelTagParts(text)
	if err != nil {
		return result, err
	}
	var failures []error
	for _, part := range parts {
		failures = append(failures, parseModelOption(&result, part, seen))
	}

	if result.Mutate != "" && result.Input == "" {
		failures = append(failures, safeerror.New("mutate requires input"))
	}
	return result, safeerror.Join("invalid foundry option", failures...)
}

func parseModelOption(result *ModelTag, part string, seen map[string]bool) error {
	if part == "" {
		return safeerror.New("empty foundry option")
	}
	if part == "-" {
		return safeerror.New("- must be the only foundry option")
	}
	key, value, assigned := strings.Cut(part, "=")
	if seen[key] {
		return safeerror.New(fmt.Sprintf("duplicate foundry option %s", key))
	}
	seen[key] = true
	if key == "slot" {
		if assigned {
			return safeerror.New("slot is a bare flag, not slot=<name>")
		}
		result.Slot = true
		return nil
	}
	switch key {
	case "column", "default", "role", "input", "mutate", "codec", "member", "key-role":
	default:
		return safeerror.New(fmt.Sprintf("unknown foundry option %s", key))
	}
	if !assigned || value == "" {
		return safeerror.New(fmt.Sprintf("foundry option requires key=value: %s", key))
	}
	switch key {
	case "column":
		if !sqlname.Valid(value) {
			return safeerror.New("column requires a SQL identifier")
		}
		result.Column = value
	case "default":
		if value != "database" {
			return safeerror.New("default requires database")
		}
		result.DatabaseDefault = true
	case "role":
		switch value {
		case "column", "text", "number", "enum", "json", "time", "interval", "created-at", "updated-at", "soft-delete":
			result.Role = value
		default:
			return safeerror.New(fmt.Sprintf("unknown role %s", value))
		}
	case "input":
		result.Input = value
	case "mutate", "codec":
		if !ModelIdentifier(value) {
			return safeerror.New(fmt.Sprintf("%s requires a named function", key))
		}
		if key == "mutate" {
			result.Mutate = value
		} else {
			result.Codec = value
		}
	case "member":
		if value != "none" && !ModelIdentifier(value) {
			return safeerror.New("member requires a Go identifier or none")
		}
		result.Member = value
	case "key-role":
		switch value {
		case "lookup", "primary", "input", "number", "text", "primary-number", "primary-text":
			result.KeyRole = value
		default:
			return safeerror.New(fmt.Sprintf("unknown key-role %s", value))
		}
	}
	return nil
}

// ModelIdentifier excludes keywords and the blank identifier from generated names.
func ModelIdentifier(name string) bool { return name != "_" && token.IsIdentifier(name) }

// NearFoundry reports a likely misspelling of the declaration namespace.
func NearFoundry(key string) bool {
	const want = "foundry"
	if len(key) > len(want)+1 && strings.HasPrefix(key, want) && strings.ContainsRune("_-.", rune(key[len(want)])) {
		return true
	}
	if len(key) < len(want)-1 || len(key) > len(want)+1 {
		return false
	}
	if len(key) == len(want) {
		for i := 0; i < len(want)-1; i++ {
			if key[:i] == want[:i] && key[i] == want[i+1] && key[i+1] == want[i] && key[i+2:] == want[i+2:] {
				return true
			}
		}
	}
	previous := make([]int, len(want)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := range key {
		current := make([]int, len(want)+1)
		current[0] = i + 1
		for j := range want {
			cost := 0
			if key[i] != want[j] {
				cost = 1
			}
			current[j+1] = min(previous[j+1]+1, current[j]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(want)] <= 1
}

// Commas inside concrete Go input types are not option separators.
func modelTagParts(text string) ([]string, error) {
	var result []string
	var stack []byte
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\'', '"', '`':
			quoted, err := strconv.QuotedPrefix(text[i:])
			if err != nil {
				return nil, safeerror.New("malformed foundry option: invalid quoted value")
			}
			i += len(quoted) - 1
		case '[', '(', '{':
			stack = append(stack, text[i])
		case ']', ')', '}':
			if len(stack) == 0 {
				return nil, safeerror.New("malformed foundry option: unmatched delimiters")
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if open == '[' && text[i] != ']' || open == '(' && text[i] != ')' || open == '{' && text[i] != '}' {
				return nil, safeerror.New("malformed foundry option: unmatched delimiters")
			}
		case ',':
			if len(stack) == 0 {
				result = append(result, text[start:i])
				start = i + 1
			}
		}
	}
	if len(stack) != 0 {
		return nil, safeerror.New("malformed foundry option: unmatched delimiters")
	}
	return append(result, text[start:]), nil
}
