package enum

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// LabelKey uses the same case list as enum membership and client metadata.
func (d Descriptor[E]) LabelKey(value E) (i18n.MessageKey, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	for _, c := range d.cases {
		if c.Value == value {
			if c.LabelKey != "" {
				return c.LabelKey, nil
			}
			break
		}
	}
	return "", fault.New(fault.Missing, "enum value has no declared localization label")
}
func (d Descriptor[E]) Label(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID, value E) (string, error) {
	key, err := d.LabelKey(value)
	if err != nil {
		return "", err
	}
	return catalog.Label(ctx, locale, key)
}

// LabelDefinitions contributes parameter-free declarations without another enum
// value list. Unlabeled cases are omitted and shared keys contribute once, in
// first-case order. Callers may combine these with typed message definitions
// when constructing one catalog.
func (d Descriptor[E]) LabelDefinitions() ([]i18n.MessageDefinition, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	var result []i18n.MessageDefinition
	seen := make(map[i18n.MessageKey]bool)
	for _, c := range d.cases {
		if c.LabelKey != "" && !seen[c.LabelKey] {
			seen[c.LabelKey] = true
			result = append(result, i18n.MessageDefinition{Key: c.LabelKey})
		}
	}
	return result, nil
}
