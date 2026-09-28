package config

import (
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

const MaxTableEntries = namedservice.MaxEntries
const MaxTableBytes = 1 << 20
const maxObjectDepth = 64

// DecodeObject converts a normalized object into a schema-owned layer. It is
// shared by TOML and nested named tables; field decoding remains owned by Schema.
// Supported scalars are strings, booleans, int64, float64 and json.Number.
// Collections at declared keys become JSON for that key's codec. Dotted object
// keys are rejected rather than ambiguously reinterpreted as namespace paths.
func DecodeObject[T any](object map[string]any, schema *Schema[T], source string) (Values, error) {
	if schema == nil || object == nil || strings.TrimSpace(source) == "" {
		return Values{}, fault.New(fault.Invalid, "configuration object needs a schema, object and source")
	}
	fields, namespaces := make(map[string]bool), make(map[string]bool)
	for _, name := range schema.Names() {
		fields[name] = true
		for index := strings.LastIndexByte(name, '.'); index >= 0; index = strings.LastIndexByte(name, '.') {
			name = name[:index]
			namespaces[name] = true
		}
	}
	result := Values{Name: source, Data: make(map[string]string)}
	var flatten func(map[string]any, string, int) error
	flatten = func(table map[string]any, prefix string, depth int) error {
		if depth > maxObjectDepth {
			return fault.New(fault.Invalid, "configuration nesting exceeds limit")
		}
		for _, key := range objectKeys(table) {
			if strings.ContainsRune(key, '.') {
				return fault.New(fault.Invalid, "configuration namespace segments cannot contain dots")
			}
			name, value := prefix+key, table[key]
			if fields[name] {
				text, err := objectText(value)
				if err != nil {
					return err
				}
				result.Data[name] = text
				continue
			}
			nested, ok := value.(map[string]any)
			if !namespaces[name] || !ok {
				return fault.New(fault.Invalid, "configuration contains an undeclared setting or invalid namespace")
			}
			if err := flatten(nested, name+".", depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := flatten(object, "", 0); err != nil {
		return Values{}, err
	}
	return result, nil
}
func objectText(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case float64:
		data, err := json.Marshal(value)
		return string(data), err
	case json.Number:
		return value.String(), nil
	case bool:
		return strconv.FormatBool(value), nil
	default:
		data, err := json.Marshal(value)
		return string(data), err
	}
}
func objectKeys[V any](table map[string]V) []string {
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// DecodeTable decodes named objects using the SAME generated element schema as
// ordinary settings. Durations remain duration strings, secrets retain their
// codec and unknown fields fail. Defaults is called once for every input name.
// Each supplied table REPLACES the whole collection: it does not merge entries
// with earlier layers. Typed overrides also replace the collection. Empty maps
// are allowed; an enabled service family's validator requires its default.
// Names follow the service identifier grammar. Duplicate JSON keys, null,
// oversized input and excessive depth fail without a partial result.
func DecodeTable[K ~string, V any](raw string, schema *Schema[V], defaults func(K) V, validate func(V) error) (map[K]V, error) {
	if schema == nil || defaults == nil || len(raw) > MaxTableBytes {
		return nil, fault.New(fault.Invalid, "invalid named configuration table")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	decoded, err := readObjectValue(decoder, 0)
	if err != nil {
		return nil, fault.New(fault.Invalid, "invalid named configuration table")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, fault.New(fault.Invalid, "named configuration requires one object")
	}
	object, ok := decoded.(map[string]any)
	if !ok || len(object) > MaxTableEntries {
		return nil, fault.New(fault.Invalid, "named configuration requires a bounded object")
	}
	result := make(map[K]V, len(object))
	for _, name := range objectKeys(object) {
		if !identifier.Semantic(name) {
			return nil, fault.New(fault.Invalid, "invalid configured service name")
		}
		entry, ok := object[name].(map[string]any)
		if !ok {
			return nil, fault.New(fault.Invalid, "named configuration entry must be an object")
		}
		layer, err := DecodeObject(entry, schema, "named configuration")
		if err != nil {
			return nil, err
		}
		value, _, err := schema.Load(defaults(K(name)), Inputs[V]{Files: []Values{layer}, Validate: validate})
		if err != nil {
			return nil, err
		}
		result[K(name)] = value
	}
	return result, nil
}

// Token parsing preserves integer precision and rejects duplicates at every
// depth. encoding/json's map decoder would silently accept the last duplicate.
func readObjectValue(d *json.Decoder, depth int) (any, error) {
	if depth > maxObjectDepth {
		return nil, fault.New(fault.Invalid, "configuration nesting exceeds limit")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, collection := token.(json.Delim)
	if !collection {
		return token, nil
	}
	switch delim {
	case '{':
		result := make(map[string]any)
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := token.(string)
			if !ok {
				return nil, fault.New(fault.Invalid, "invalid object key")
			}
			if _, exists := result[key]; exists {
				return nil, fault.New(fault.Duplicate, "duplicate configuration key")
			}
			value, err := readObjectValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fault.New(fault.Invalid, "invalid configuration object")
		}
		return result, nil
	case '[':
		result := make([]any, 0)
		for d.More() {
			value, err := readObjectValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fault.New(fault.Invalid, "invalid configuration array")
		}
		return result, nil
	default:
		return nil, fault.New(fault.Invalid, "invalid configuration delimiter")
	}
}
