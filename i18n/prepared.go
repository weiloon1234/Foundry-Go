package i18n

import (
	"context"
	"maps"
	"slices"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"golang.org/x/text/language"
)

// englishTag selects the plural rules of every prepared English fallback.
var englishTag = language.English

// MessageArgument is a serialized declaration argument, never an input capture.
// Numbers retain decimal text instead of passing through float64.
type MessageArgument struct {
	Name  string        `json:"name"`
	Kind  ParameterKind `json:"kind"`
	Value string        `json:"value"`
}

// MessageRecipe is the owned metadata for a prepared message. Fallback uses
// English plural forms, including Other; nonplural messages have only Other.
type MessageRecipe struct {
	Definition      MessageDefinition     `json:"definition"`
	Arguments       []MessageArgument     `json:"arguments,omitempty"`
	Fallback        map[PluralForm]string `json:"fallback"`
	LiteralFallback bool                  `json:"literal_fallback,omitempty"`
}

// PreparedMessage owns validated arguments and an already compiled English
// fallback. It is immutable and safe to share across requests. Prefer the
// generated message.Message[A].Bind adapter when authoring application messages.
type PreparedMessage struct {
	recipe   MessageRecipe
	args     map[string]Argument
	fallback compiledTemplate
}

// PrepareMessage is the explicit dynamic construction boundary. It snapshots
// its inputs and rejects incomplete signatures and invalid fallback templates.
func PrepareMessage(definition MessageDefinition, args map[string]Argument, fallback Template) (PreparedMessage, error) {
	if err := definition.Validate(); err != nil {
		return PreparedMessage{}, err
	}
	if len(args) != len(definition.Parameters) {
		return PreparedMessage{}, argumentError(definition.Key, "", "the argument count differs from the declaration")
	}
	owned := cloneDefinition(definition)
	slices.SortFunc(owned.Parameters, compareParameters)
	recipe := MessageRecipe{Definition: owned, Fallback: maps.Clone(fallback.Forms)}
	if definition.Plural == "" {
		recipe.Fallback = map[PluralForm]string{Other: fallback.Text}
	}
	total := 0
	for _, p := range owned.Parameters {
		value := args[p.Name]
		if !value.valid(p.Kind) {
			return PreparedMessage{}, argumentError(definition.Key, p.Name, "missing, wrong kind or oversized")
		}
		total += len(value.text)
		if total > MaxTextBytes {
			return PreparedMessage{}, argumentError(definition.Key, p.Name, "arguments exceed 64 KiB")
		}
		recipe.Arguments = append(recipe.Arguments, MessageArgument{Name: p.Name, Kind: p.Kind, Value: value.text})
	}
	compiled, size, problem := compileTemplate(fallback, owned)
	if problem != "" {
		return PreparedMessage{}, definitionError(definition.Key, "English fallback: "+string(problem))
	}
	if size > MaxTextBytes {
		return PreparedMessage{}, definitionError(definition.Key, "English fallback exceeds 64 KiB")
	}
	return PreparedMessage{recipe: recipe, args: maps.Clone(args), fallback: compiled}, nil
}

// Prepare validates an exported recipe without retaining caller-owned data.
func (r MessageRecipe) Prepare() (PreparedMessage, error) {
	if len(r.Arguments) > MaxParameters || len(r.Fallback) > 6 {
		return PreparedMessage{}, invalidMessage()
	}
	args := make(map[string]Argument, len(r.Arguments))
	for _, item := range r.Arguments {
		if _, exists := args[item.Name]; exists {
			return PreparedMessage{}, invalidMessage()
		}
		value := Argument{kind: item.Kind, text: item.Value}
		if item.Kind == NumberParameter {
			number, err := decimal.Parse(item.Value)
			if err != nil {
				return PreparedMessage{}, err
			}
			value = Number(number)
		} else if item.Kind == BooleanParameter && item.Value != "true" && item.Value != "false" {
			return PreparedMessage{}, invalidMessage()
		}
		args[item.Name] = value
	}
	if r.LiteralFallback {
		text, ok := r.Fallback[Other]
		if !ok || len(r.Fallback) != 1 {
			return PreparedMessage{}, invalidMessage()
		}
		return PrepareLiteralMessage(r.Definition, args, text)
	}
	fallback := Template{Forms: r.Fallback}
	if r.Definition.Plural == "" {
		text, ok := r.Fallback[Other]
		if !ok || len(r.Fallback) != 1 {
			return PreparedMessage{}, invalidMessage()
		}
		fallback = Template{Text: text}
	}
	return PrepareMessage(r.Definition, args, fallback)
}

func (m PreparedMessage) Validate() error {
	if m.fallback == nil {
		return invalidMessage()
	}
	return nil
}
func (m PreparedMessage) Description() MessageRecipe {
	r := m.recipe
	r.Definition = cloneDefinition(r.Definition)
	r.Arguments = slices.Clone(r.Arguments)
	r.Fallback = maps.Clone(r.Fallback)
	return r
}

// WithText replaces an existing text argument in a new message. Missing names
// are ignored, letting a presenter supply optional standard label arguments.
// The immutable definition and fallback are shared; only arguments are copied.
func (m PreparedMessage) WithText(name, value string) (PreparedMessage, error) {
	if m.Validate() != nil || !validText(value) {
		return PreparedMessage{}, invalidMessage()
	}
	current, exists := m.args[name]
	if !exists {
		return m, nil
	}
	if current.kind != TextParameter {
		return PreparedMessage{}, argumentError(m.recipe.Definition.Key, name, "only text arguments can be replaced")
	}
	total := len(value)
	for key, arg := range m.args {
		if key != name {
			total += len(arg.text)
		}
	}
	if total > MaxTextBytes {
		return PreparedMessage{}, argumentError(m.recipe.Definition.Key, name, "arguments exceed 64 KiB")
	}
	m.args = maps.Clone(m.args)
	m.args[name] = Text(value)
	m.recipe.Arguments = slices.Clone(m.recipe.Arguments)
	for i := range m.recipe.Arguments {
		if m.recipe.Arguments[i].Name == name {
			m.recipe.Arguments[i].Value = value
		}
	}
	return m, nil
}

// Format tries the supplied catalog (requested locale, supported parents, then
// its configured fallback), then this message's English fallback. An
// unregistered key also uses the English fallback; a conflicting registered
// signature is an error. Nil catalog selects English. Arguments were validated
// at preparation, so formatting only checks the catalog signature once.
func (m PreparedMessage) Format(ctx context.Context, catalog *Catalog, locale LocaleID) (Result, error) {
	if m.Validate() != nil || ctx == nil {
		return Result{}, invalidMessage()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if catalog != nil {
		if catalog.Validate() != nil || !catalog.locales.Contains(locale) {
			return Result{}, invalidMessage()
		}
		if _, exists := catalog.definitions[m.recipe.Definition.Key]; exists {
			if err := catalog.acceptsSorted(m.recipe.Definition); err != nil {
				return Result{}, err
			}
			result, err := catalog.format(ctx, locale, m.recipe.Definition, m.args)
			if err != nil || !result.Missing {
				return result, err
			}
		}
	}
	form := Other
	if m.recipe.Definition.Plural != "" {
		form = pluralForm(englishTag, m.recipe.Definition.Kind, m.args[m.recipe.Definition.Plural].text)
	}
	parts, exists := m.fallback[form]
	if !exists {
		parts = m.fallback[Other]
	}
	text, err := renderTemplate(parts, m.args)
	if err != nil {
		return Result{}, argumentError(m.recipe.Definition.Key, "", "the rendered message exceeds 64 KiB")
	}
	return Result{Text: text, Locale: "en", Fallback: locale != "" && locale != "en"}, nil
}

// Definition returns an owned signature and whether it is registered.
func (c *Catalog) Definition(key MessageKey) (MessageDefinition, bool) {
	if c == nil {
		return MessageDefinition{}, false
	}
	d, ok := c.definitions[key]
	return cloneDefinition(d), ok
}

// PrepareLiteralMessage preserves text literally, including template delimiters.
func PrepareLiteralMessage(definition MessageDefinition, args map[string]Argument, text string) (PreparedMessage, error) {
	if !validText(text) {
		return PreparedMessage{}, invalidMessage()
	}
	empty := Template{}
	if definition.Plural != "" {
		empty = Template{Forms: map[PluralForm]string{Other: ""}}
	}
	prepared, err := PrepareMessage(definition, args, empty)
	if err != nil {
		return PreparedMessage{}, err
	}
	prepared.recipe.Fallback = map[PluralForm]string{Other: text}
	prepared.recipe.LiteralFallback = true
	prepared.fallback = compiledTemplate{Other: []templatePart{{text: text}}}
	return prepared, nil
}
