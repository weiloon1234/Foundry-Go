// Package storage supplies typed object storage with explicit resource bounds,
// provider capabilities and mutation outcomes. Applications use Disk; adapters
// implement Backend without buffering an entire unknown-length object.
package storage

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

const MaxKeyBytes = 1024

// DiskID is an explicit registered disk, not an object key or filesystem path.
type DiskID string

func (d DiskID) Validate() error {
	if !identifier.Semantic(string(d)) {
		return fault.New(fault.Invalid, "invalid storage disk identifier")
	}
	return nil
}

// ObjectKey is a validated, case-sensitive UTF-8 object name. It is never a
// filesystem path or URL. Parsing rejects traversal and ambiguous separators;
// it never cleans, case-folds, percent-decodes or Unicode-normalizes a key.
type ObjectKey struct{ text string }

func ParseKey(text string) (ObjectKey, error) {
	if err := validateName(text, false); err != nil {
		return ObjectKey{}, err
	}
	return ObjectKey{text: strings.Clone(text)}, nil
}
func (k ObjectKey) String() string  { return k.text }
func (k ObjectKey) IsZero() bool    { return k.text == "" }
func (k ObjectKey) Validate() error { return validateName(k.text, false) }
func (k ObjectKey) MarshalText() ([]byte, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return []byte(k.text), nil
}
func (k *ObjectKey) UnmarshalText(text []byte) error {
	if k == nil {
		return fault.New(fault.Invalid, "missing object key destination")
	}
	*k = ObjectKey{}
	parsed, err := ParseKey(string(text))
	if err == nil {
		*k = parsed
	}
	return err
}
func (k ObjectKey) MarshalJSON() ([]byte, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(k.text)
}
func (k *ObjectKey) UnmarshalJSON(data []byte) error {
	if k == nil {
		return fault.New(fault.Invalid, "missing object key destination")
	}
	*k = ObjectKey{}
	if len(data) > MaxKeyBytes*6+2 {
		return fault.New(fault.Invalid, "object key JSON exceeds its limit")
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fault.New(fault.Invalid, "object key requires a JSON string")
	}
	return k.UnmarshalText([]byte(text))
}
func (ObjectKey) JSONContract() contract.JSON[ObjectKey] {
	t := reflect.TypeFor[ObjectKey]()
	id := contract.TypeID(t.PkgPath() + "." + t.Name())
	return contract.DefineJSONValue[ObjectKey](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}

// Prefix selects an exact lexical prefix, not a directory or a normalized path.
// Zero means every object. A trailing slash is allowed for directory-like use.
type Prefix struct{ text string }

func ParsePrefix(text string) (Prefix, error) {
	if err := validateName(text, true); err != nil {
		return Prefix{}, err
	}
	return Prefix{text: strings.Clone(text)}, nil
}
func (p Prefix) String() string  { return p.text }
func (p Prefix) Validate() error { return validateName(p.text, true) }
func (p Prefix) Contains(key ObjectKey) bool {
	return key.Validate() == nil && strings.HasPrefix(key.text, p.text)
}
func validateName(text string, prefix bool) error {
	invalid := func() error { return fault.New(fault.Invalid, "invalid object key or prefix") }
	if len(text) > MaxKeyBytes || !utf8.ValidString(text) {
		return invalid()
	}
	if text == "" {
		if prefix {
			return nil
		}
		return invalid()
	}
	if strings.HasPrefix(text, "/") || strings.Contains(text, "\\") {
		return invalid()
	}
	for _, c := range text {
		if c < 32 || c == 127 {
			return invalid()
		}
	}
	parts := strings.Split(text, "/")
	for i, part := range parts {
		if part == "" && prefix && i == len(parts)-1 {
			continue
		}
		if part == "" || part == "." || part == ".." {
			return invalid()
		}
	}
	return nil
}
