package config

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ValidateNamespace uses the same grammar as configuration field names.
func ValidateNamespace(namespace string) error {
	if len(namespace) > 256 || !settingName.MatchString(namespace) {
		return fault.New(fault.Invalid, "invalid configuration namespace")
	}
	return nil
}

// WithinNamespace requires every key to be a descendant of the selected
// namespace. It never rewrites keys or silently drops unknown input values.
func (s *Schema[T]) WithinNamespace(namespace string) error {
	if err := ValidateNamespace(namespace); err != nil {
		return err
	}
	if s == nil || s.byName == nil {
		return fault.New(fault.Invalid, "configuration schema is not initialized")
	}
	for _, field := range s.fields {
		if !strings.HasPrefix(field.name(), namespace+".") {
			return fault.New(fault.Invalid, "configuration field is outside its declared namespace")
		}
	}
	return nil
}
