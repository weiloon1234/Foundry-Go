package i18n

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// DiscoverDefinitions reads a catalog tree with ReadTemplates and discovers
// dynamic message definitions from its template placeholders and plural forms.
// An explicit LocaleCatalog is required; all declared supported locales must be present.
func DiscoverDefinitions(ctx context.Context, source fs.FS, locales LocaleCatalog) ([]MessageDefinition, error) {
	if ctx == nil || source == nil {
		return nil, invalidMessage()
	}
	if locales == nil {
		return nil, invalidLocale()
	}
	set, err := SnapshotLocales(ctx, locales)
	if err != nil {
		return nil, err
	}
	templates, err := ReadTemplates(ctx, source, set)
	if err != nil {
		return nil, err
	}
	for _, id := range set.Locales() {
		if _, exists := templates[id]; !exists {
			return nil, catalogError(id, "", "", "declared supported locale "+boundedQuote(string(id))+" is missing from frontend catalog")
		}
	}
	type keyData struct {
		isPlural    bool
		pluralParam string
		parameters  map[string]bool
	}
	keys := make(map[MessageKey]*keyData)

	for _, localeMap := range templates {
		for key, tmpl := range localeMap {
			data, exists := keys[key]
			if !exists {
				data = &keyData{parameters: make(map[string]bool)}
				keys[key] = data
			}
			if len(tmpl.Forms) > 0 {
				data.isPlural = true
				var formPlaceholders []string
				for _, formText := range tmpl.Forms {
					parts, problem := parseTemplate(formText, nil)
					if problem != "" {
						return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("invalid template for message %q: %s", string(key), string(problem)))
					}
					for _, part := range parts {
						if part.parameter != "" {
							data.parameters[part.parameter] = true
							if !slices.Contains(formPlaceholders, part.parameter) {
								formPlaceholders = append(formPlaceholders, part.parameter)
							}
						}
					}
				}
				if data.pluralParam == "" {
					if slices.Contains(formPlaceholders, "count") {
						data.pluralParam = "count"
					} else if len(formPlaceholders) == 1 {
						data.pluralParam = formPlaceholders[0]
					} else {
						for _, candidate := range []string{"n", "quantity", "items", "total", "minutes", "hours", "days"} {
							if slices.Contains(formPlaceholders, candidate) {
								data.pluralParam = candidate
								break
							}
						}
						if data.pluralParam == "" && len(formPlaceholders) > 0 {
							data.pluralParam = formPlaceholders[0]
						}
					}
				}
			} else if tmpl.Text != "" {
				parts, problem := parseTemplate(tmpl.Text, nil)
				if problem != "" {
					return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("invalid template for message %q: %s", string(key), string(problem)))
				}
				for _, part := range parts {
					if part.parameter != "" {
						data.parameters[part.parameter] = true
					}
				}
			}
		}
	}

	orderedKeys := slices.Sorted(maps.Keys(keys))
	definitions := make([]MessageDefinition, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		data := keys[key]
		paramNames := slices.Sorted(maps.Keys(data.parameters))
		var params []Parameter
		for _, name := range paramNames {
			kind := TextParameter
			if data.isPlural && name == data.pluralParam {
				kind = NumberParameter
			}
			params = append(params, Parameter{Name: name, Kind: kind})
		}
		def := MessageDefinition{
			Key:        key,
			Parameters: params,
		}
		if data.isPlural {
			def.Plural = data.pluralParam
			def.Kind = Cardinal
		}
		if err := def.Validate(); err != nil {
			return nil, err
		}
		definitions = append(definitions, def)
	}
	return definitions, nil
}
