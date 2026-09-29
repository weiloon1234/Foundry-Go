package i18n

import (
	"cmp"
	"context"
	"slices"

	"golang.org/x/text/language"
)

// Catalog is an immutable UI-message catalog and the same LocaleCatalog used
// by model content. It creates no goroutines and owns no open resources.
type Catalog struct {
	locales     LocaleSet
	fallback    LocaleID
	definitions map[MessageKey]MessageDefinition
	messages    map[LocaleID]map[MessageKey]compiledTemplate
	// lookup is each supported locale's UI chain: itself, its supported
	// parents, then the configured fallback. tags are parsed plural locales.
	lookup map[LocaleID][]LocaleID
	tags   map[LocaleID]language.Tag
}

// CatalogOptions selects the UI fallback, independently of model-content
// LocaleSet.Fallbacks. Empty Fallback selects the configured default locale.
type CatalogOptions struct{ Fallback LocaleID }

func NewCatalog(ctx context.Context, locales LocaleCatalog, options CatalogOptions, definitions []MessageDefinition, messages map[LocaleID]map[MessageKey]Template) (*Catalog, error) {
	return newCatalog(ctx, locales, options, definitions, messages, nil)
}

// messageSources optionally records the file that supplied each message, so
// Load can name locale, file and key in construction errors.
type messageSources map[LocaleID]map[MessageKey]string

func newCatalog(ctx context.Context, locales LocaleCatalog, options CatalogOptions, definitions []MessageDefinition, messages map[LocaleID]map[MessageKey]Template, sources messageSources) (*Catalog, error) {
	set, err := SnapshotLocales(ctx, locales)
	if err != nil {
		return nil, err
	}
	if options.Fallback == "" {
		options.Fallback = set.Default()
	}
	if !set.Contains(options.Fallback) {
		return nil, catalogError(options.Fallback, "", "", "the configured fallback locale is not supported")
	}
	if len(definitions) > MaxMessages {
		return nil, catalogError("", "", "", "more than 10,000 message declarations")
	}
	if len(messages) > MaxLocales {
		return nil, catalogError("", "", "", "more than 64 locales")
	}
	c := &Catalog{locales: set, fallback: options.Fallback, definitions: make(map[MessageKey]MessageDefinition, len(definitions)), messages: make(map[LocaleID]map[MessageKey]compiledTemplate, len(messages)), lookup: make(map[LocaleID][]LocaleID, len(set.ids)), tags: make(map[LocaleID]language.Tag, len(set.ids))}
	for _, id := range set.ids {
		c.lookup[id] = set.appendChain(nil, id, c.fallback)
		tag, err := language.Parse(string(id))
		if err != nil {
			return nil, catalogError(id, "", "", "the locale cannot be parsed for plural rules")
		}
		c.tags[id] = tag
	}
	total := 0
	for _, d := range definitions {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		if _, exists := c.definitions[d.Key]; exists {
			return nil, definitionError(d.Key, "the message key is declared twice")
		}
		d = cloneDefinition(d)
		slices.SortFunc(d.Parameters, func(a, b Parameter) int { return cmp.Compare(a.Name, b.Name) })
		c.definitions[d.Key] = d
		total += len(d.Key) + len(d.Plural)
		for _, p := range d.Parameters {
			total += len(p.Name) + len(p.Kind)
		}
		if total > MaxCatalogBytes {
			return nil, catalogError("", "", string(d.Key), "declarations exceed the 16 MiB catalog bound")
		}
	}
	for locale, entries := range messages {
		if !set.Contains(locale) {
			return nil, catalogError(locale, "", "", "the locale is not supported")
		}
		if len(entries) > MaxMessages {
			return nil, catalogError(locale, "", "", "more than 10,000 messages")
		}
		compiled := make(map[MessageKey]compiledTemplate, len(entries))
		for key, t := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			file := sources[locale][key]
			d, exists := c.definitions[key]
			if !exists {
				return nil, catalogError(locale, file, string(key), "the message is not declared")
			}
			parts, size, problem := compileTemplate(t, d)
			if problem != "" {
				return nil, catalogError(locale, file, string(key), string(problem))
			}
			total += len(key) + size
			if total > MaxCatalogBytes {
				return nil, catalogError(locale, file, string(key), "templates exceed the 16 MiB catalog bound")
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
// Typed adapters use it before evaluating argument encoders. Declarations that
// are already name-sorted (all framework-built ones) are compared without
// copying; equality with a registered, validated declaration proves validity.
func (c *Catalog) Accepts(d MessageDefinition) error {
	if c.Validate() != nil {
		return invalidMessage()
	}
	if !slices.IsSortedFunc(d.Parameters, compareParameters) {
		d = cloneDefinition(d)
		slices.SortFunc(d.Parameters, compareParameters)
	}
	if err := c.acceptsSorted(d); err != nil {
		if invalid := d.Validate(); invalid != nil {
			return invalid
		}
		return err
	}
	return nil
}
func (c *Catalog) acceptsSorted(d MessageDefinition) error {
	registered, ok := c.definitions[d.Key]
	if !ok {
		return definitionError(d.Key, "the message is not registered in this catalog")
	}
	if !sameDefinition(registered, d) {
		return definitionError(d.Key, "the parameter or plural signature differs from the catalog registration")
	}
	return nil
}
func compareParameters(a, b Parameter) int { return cmp.Compare(a.Name, b.Name) }

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
// Lookup tries the requested locale, its supported parents (en-GB → en), then
// the configured fallback; it never tries unrelated supported locales.
func (c *Catalog) FormatDynamic(ctx context.Context, locale LocaleID, key MessageKey, args map[string]Argument) (Result, error) {
	if ctx == nil || c.Validate() != nil || !c.locales.Contains(locale) {
		return Result{}, invalidMessage()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	d, ok := c.definitions[key]
	if !ok {
		return Result{}, definitionError(key, "the message is not registered in this catalog")
	}
	if len(args) != len(d.Parameters) {
		return Result{}, argumentError(key, "", "the argument count differs from the declaration")
	}
	for _, p := range d.Parameters {
		if !args[p.Name].valid(p.Kind) {
			return Result{}, argumentError(key, p.Name, "missing, wrong kind or oversized")
		}
	}
	return c.format(ctx, locale, d, args)
}

// format renders already validated arguments; callers own signature checks.
func (c *Catalog) format(ctx context.Context, locale LocaleID, d MessageDefinition, args map[string]Argument) (Result, error) {
	var template compiledTemplate
	var selected LocaleID
	for _, candidate := range c.lookup[locale] {
		if found, ok := c.messages[candidate][d.Key]; ok {
			template, selected = found, candidate
			break
		}
	}
	if template == nil {
		return Result{Text: string(d.Key), Missing: true}, nil
	}
	form := Other
	if d.Plural != "" {
		form = pluralForm(c.tags[selected], d.Kind, args[d.Plural].text)
	}
	parts, ok := template[form]
	if !ok {
		parts = template[Other]
	}
	text, err := renderTemplate(parts, args)
	if err != nil {
		return Result{}, argumentError(d.Key, "", "the rendered message exceeds 64 KiB")
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
