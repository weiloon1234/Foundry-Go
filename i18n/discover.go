package i18n

import (
	"context"
	"io/fs"
	"maps"
	"slices"
)

// DiscoverDefinitions reads a catalog tree with ReadTemplates and discovers
// dynamic message definitions from its template placeholders and plural forms.
// An explicit LocaleCatalog is required; all declared supported locales must be present.
// Plural forms across all locales must contain exactly one distinct placeholder,
// which becomes numeric. Messages needing additional plural-form arguments must
// use an explicit MessageDefinition.Plural in a shared catalog instead.
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
		isPlural         bool
		pluralParameters map[string]bool
		parameters       map[string]bool
	}
	keys := make(map[MessageKey]*keyData)

	for locale, localeMap := range templates {
		for key, tmpl := range localeMap {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			data, exists := keys[key]
			if !exists {
				data = &keyData{parameters: make(map[string]bool), pluralParameters: make(map[string]bool)}
				keys[key] = data
			}
			if len(tmpl.Forms) > 0 {
				data.isPlural = true
				for _, formText := range tmpl.Forms {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					parts, problem := parseTemplate(formText, nil)
					if problem != "" {
						return nil, catalogError(locale, "", string(key), string(problem))
					}
					for _, part := range parts {
						if part.parameter != "" {
							data.parameters[part.parameter] = true
							data.pluralParameters[part.parameter] = true
						}
					}
				}
			} else if tmpl.Text != "" {
				parts, problem := parseTemplate(tmpl.Text, nil)
				if problem != "" {
					return nil, catalogError(locale, "", string(key), string(problem))
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data := keys[key]
		pluralParam := ""
		if data.isPlural {
			if len(data.pluralParameters) != 1 {
				return nil, definitionError(key, "frontend plural discovery requires exactly one placeholder across all plural forms and locales; use a single numeric placeholder or move the message to a shared catalog with an explicit MessageDefinition.Plural")
			}
			for name := range data.pluralParameters {
				pluralParam = name
			}
		}
		paramNames := slices.Sorted(maps.Keys(data.parameters))
		var params []Parameter
		for _, name := range paramNames {
			kind := TextParameter
			if name == pluralParam {
				kind = NumberParameter
			}
			params = append(params, Parameter{Name: name, Kind: kind})
		}
		def := MessageDefinition{
			Key:        key,
			Parameters: params,
		}
		if data.isPlural {
			def.Plural = pluralParam
			def.Kind = Cardinal
		}
		if err := def.Validate(); err != nil {
			return nil, err
		}
		definitions = append(definitions, def)
	}
	return definitions, nil
}
