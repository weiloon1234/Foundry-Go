// Package enum describes generated named string and integer values without
// erasing their Go type. Transport manifests are an explicit serialization boundary.
package enum

import (
	"encoding/json"
	"fmt"
	"go/token"
	"reflect"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/i18n"
)

// Scalar is the set of supported enum underlying types. Platform pointers and
// floating-point values are not enum values.
type Scalar interface {
	~string | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// Case retains a declared constant's Go name and concrete enum value.
type Case[E Scalar] struct {
	Name     string
	Value    E
	LabelKey i18n.MessageKey
}

// Descriptor is an immutable enum declaration. Its zero value is invalid.
type Descriptor[E Scalar] struct {
	packagePath, name string
	cases             []Case[E]
}

// Describe is a declaration boundary used by generated code. It copies cases;
// registries and custom declarations must Validate before accepting a descriptor.
func Describe[E Scalar](packagePath, name string, cases ...Case[E]) Descriptor[E] {
	return Descriptor[E]{packagePath, name, slices.Clone(cases)}
}

func (d Descriptor[E]) PackagePath() string { return d.packagePath }
func (d Descriptor[E]) Name() string        { return d.name }
func (d Descriptor[E]) Cases() []Case[E]    { return slices.Clone(d.cases) }
func (d Descriptor[E]) Contains(value E) bool {
	for _, c := range d.cases {
		if c.Value == value {
			return true
		}
	}
	return false
}

// Validate checks descriptor names, values and their actual JSON representation.
// It does not infer public API exposure from a persistence model.
func (d Descriptor[E]) Validate() error {
	if strings.TrimSpace(d.packagePath) == "" || strings.ContainsAny(d.packagePath, " \t\r\n\\") || !token.IsIdentifier(d.name) || !token.IsExported(d.name) || len(d.cases) == 0 {
		return fmt.Errorf("invalid enum declaration")
	}
	names := make(map[string]bool)
	values := make(map[E]bool)
	wireValues := make(map[string]bool)
	for _, c := range d.cases {
		if !token.IsIdentifier(c.Name) || names[c.Name] || values[c.Value] {
			return fmt.Errorf("invalid or duplicate enum case")
		}
		if c.LabelKey != "" && c.LabelKey.Validate() != nil {
			return fmt.Errorf("invalid enum label key")
		}
		data, err := json.Marshal(c.Value)
		if err != nil {
			return fmt.Errorf("invalid enum wire value: %w", err)
		}
		if string(data) == "null" || wireValues[string(data)] {
			return fmt.Errorf("invalid or duplicate serialized enum value")
		}
		// A custom marshaler must not turn a named scalar into an object, a
		// stringified number, a lossy number, or a different enum value.
		if reflect.TypeFor[E]().Kind() == reflect.String {
			if len(data) == 0 || data[0] != '"' {
				return fmt.Errorf("string enum must serialize as a JSON string")
			}
		} else {
			for i, b := range data {
				if (b < '0' || b > '9') && !(i == 0 && b == '-') {
					return fmt.Errorf("integer enum must serialize as an exact JSON integer")
				}
			}
		}
		var decoded E
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != c.Value {
			return fmt.Errorf("enum serialization does not preserve its value")
		}
		names[c.Name] = true
		values[c.Value] = true
		wireValues[string(data)] = true
	}
	return nil
}

// Definition is the normalized serialized enum boundary for contract emitters.
// Values retain exact JSON numbers instead of passing through float64.
type Definition struct {
	PackagePath string     `json:"package"`
	Name        string     `json:"name"`
	Cases       []WireCase `json:"cases"`
}
type WireCase struct {
	Name     string          `json:"name"`
	Value    json.RawMessage `json:"value"`
	LabelKey i18n.MessageKey `json:"label_key,omitempty"`
}

func (d Descriptor[E]) Definition() (Definition, error) {
	if err := d.Validate(); err != nil {
		return Definition{}, err
	}
	result := Definition{PackagePath: d.packagePath, Name: d.name, Cases: make([]WireCase, 0, len(d.cases))}
	for _, c := range d.cases {
		data, err := json.Marshal(c.Value)
		if err != nil {
			return Definition{}, err
		}
		result.Cases = append(result.Cases, WireCase{Name: c.Name, Value: data, LabelKey: c.LabelKey})
	}
	return result, nil
}
