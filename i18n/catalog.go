package i18n

import (
	"cmp"
	"context"
	"slices"
)

// Catalog is an immutable UI-message catalog and the same LocaleCatalog used
// by model content. It creates no goroutines and owns no open resources.
type Catalog struct {
	locales     LocaleSet
	fallback    LocaleID
	definitions map[MessageKey]MessageDefinition
	messages    map[LocaleID]map[MessageKey]compiledTemplate
}

// CatalogOptions selects the UI fallback, independently of model-content
// LocaleSet.Fallbacks. Empty Fallback selects the configured default locale.
type CatalogOptions struct{ Fallback LocaleID }

func NewCatalog(ctx context.Context, locales LocaleCatalog, options CatalogOptions, definitions []MessageDefinition, messages map[LocaleID]map[MessageKey]Template) (*Catalog, error) {
	set, err := SnapshotLocales(ctx, locales)
	if err != nil {
		return nil, err
	}
	if options.Fallback == "" {
		options.Fallback = set.Default()
	}
	if !set.Contains(options.Fallback) || len(definitions) > MaxMessages || len(messages) > MaxLocales {
		return nil, invalidMessage()
	}
	c := &Catalog{locales: set, fallback: options.Fallback, definitions: make(map[MessageKey]MessageDefinition, len(definitions)), messages: make(map[LocaleID]map[MessageKey]compiledTemplate, len(messages))}
	total := 0
	for _, d := range definitions {
		if d.Validate() != nil {
			return nil, invalidMessage()
		}
		if _, exists := c.definitions[d.Key]; exists {
			return nil, invalidMessage()
		}
		d = cloneDefinition(d)
		slices.SortFunc(d.Parameters, func(a, b Parameter) int { return cmp.Compare(a.Name, b.Name) })
		c.definitions[d.Key] = d
		total += len(d.Key) + len(d.Plural)
		for _, p := range d.Parameters {
			total += len(p.Name) + len(p.Kind)
		}
		if total > MaxCatalogBytes {
			return nil, invalidMessage()
		}
	}
	for locale, entries := range messages {
		if !set.Contains(locale) || len(entries) > MaxMessages {
			return nil, invalidMessage()
		}
		compiled := make(map[MessageKey]compiledTemplate, len(entries))
		for key, t := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			d, exists := c.definitions[key]
			if !exists {
				return nil, invalidMessage()
			}
			parts, size, err := compileTemplate(t, d)
			if err != nil {
				return nil, err
			}
			total += len(key) + size
			if total > MaxCatalogBytes {
				return nil, invalidMessage()
			}
			compiled[key] = parts
		}
		c.messages[locale] = compiled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Catalog) Validate() error {
	if c == nil || c.locales.Validate() != nil || !c.locales.Contains(c.fallback) {
		return invalidMessage()
	}
	return nil
}
func (c *Catalog) Snapshot(ctx context.Context) (LocaleSet, error) {
	if err := c.Validate(); err != nil {
		return LocaleSet{}, err
	}
	return c.locales.Snapshot(ctx)
}
func (c *Catalog) Definitions() []MessageDefinition {
	if c == nil {
		return nil
	}
	result := make([]MessageDefinition, 0, len(c.definitions))
	for _, d := range c.definitions {
		result = append(result, cloneDefinition(d))
	}
	slices.SortFunc(result, func(a, b MessageDefinition) int { return cmp.Compare(a.Key, b.Key) })
	return result
}

// Accepts checks the complete parameter/plural signature, not just key identity.
// Typed adapters use it before evaluating argument encoders.
func (c *Catalog) Accepts(d MessageDefinition) error {
	if c.Validate() != nil || d.Validate() != nil {
		return invalidMessage()
	}
	d = cloneDefinition(d)
	slices.SortFunc(d.Parameters, func(a, b Parameter) int { return cmp.Compare(a.Name, b.Name) })
	registered, ok := c.definitions[d.Key]
	if !ok || !sameDefinition(registered, d) {
		return invalidMessage()
	}
	return nil
}

// Result makes missing/fallback diagnostics explicit without logging values or
// aborting unrelated requests. Locale is empty only when no translation exists.
type Result struct {
	Text     string
	Locale   LocaleID
	Missing  bool
	Fallback bool
}

// FormatDynamic is the explicit runtime-key boundary. All arguments are required
// and must exactly match the registered signature, even for a missing message.
func (c *Catalog) FormatDynamic(ctx context.Context, locale LocaleID, key MessageKey, args map[string]Argument) (Result, error) {
	if ctx == nil || c.Validate() != nil || !c.locales.Contains(locale) {
		return Result{}, invalidMessage()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	d, ok := c.definitions[key]
	if !ok || len(args) != len(d.Parameters) {
		return Result{}, invalidMessage()
	}
	for _, p := range d.Parameters {
		if !args[p.Name].valid(p.Kind) {
			return Result{}, invalidMessage()
		}
	}
	selected := locale
	template, ok := c.messages[selected][key]
	if !ok {
		selected = c.fallback
		template, ok = c.messages[selected][key]
	}
	if !ok {
		return Result{Text: string(key), Missing: true}, nil
	}
	form := Other
	if d.Plural != "" {
		form = pluralForm(selected, d.Kind, args[d.Plural].text)
	}
	parts, ok := template[form]
	if !ok {
		parts = template[Other]
	}
	text, err := renderTemplate(parts, args)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{Text: text, Locale: selected, Fallback: selected != locale}, nil
}

// Label resolves a registered parameter-free message for feature metadata.
func (c *Catalog) Label(ctx context.Context, locale LocaleID, key MessageKey) (string, error) {
	r, err := c.FormatDynamic(ctx, locale, key, nil)
	return r.Text, err
}
