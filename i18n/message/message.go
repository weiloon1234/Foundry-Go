// Package message connects generated argument structs to i18n through the same
// typed JSON descriptor used by runtime codecs and client contract exporters.
package message

import (
	"cmp"
	"context"
	"encoding/json"
	"io/fs"
	"slices"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type Options struct {
	Plural string
	Kind   i18n.PluralKind
}
type Message[A any] struct {
	definition i18n.MessageDefinition
	arguments  contract.JSON[A]
	err        error
}

// Describe derives message parameters from the shared wire graph. Message
// parameters are required nonnullable text, booleans, integers or exact decimal
// strings. Floating-point and structured arguments are deliberately rejected.
func Describe(key i18n.MessageKey, schema contract.Schema, options Options) (i18n.MessageDefinition, error) {
	result := i18n.MessageDefinition{Key: key, Plural: options.Plural, Kind: options.Kind}
	if result.Plural != "" && result.Kind == "" {
		result.Kind = i18n.Cardinal
	}
	properties, err := schema.ScalarProperties()
	if err != nil || len(properties) > i18n.MaxParameters {
		return result, invalid()
	}
	for _, property := range properties {
		if !property.Property.Required || property.Value.Nullable || !i18n.ParameterName(property.Property.Name) {
			return result, invalid()
		}
		kind := i18n.TextParameter
		switch property.Value.Kind {
		case contract.StringKind:
			if property.Value.Format == contract.DecimalFormat {
				kind = i18n.NumberParameter
			}
		case contract.IntegerKind:
			kind = i18n.NumberParameter
		case contract.BooleanKind:
			kind = i18n.BooleanParameter
		default:
			return result, invalid()
		}
		result.Parameters = append(result.Parameters, i18n.Parameter{Name: property.Property.Name, Kind: kind})
	}
	slices.SortFunc(result.Parameters, func(a, b i18n.Parameter) int { return cmp.Compare(a.Name, b.Name) })
	if err := result.Validate(); err != nil {
		return result, err
	}
	return result, nil
}

// Define is the construction boundary emitted by foundry generate.
func Define[A any](key i18n.MessageKey, arguments contract.JSON[A], options Options) Message[A] {
	result := Message[A]{arguments: arguments}
	schema, err := arguments.Description()
	if err != nil {
		result.err = err
		return result
	}
	result.definition, result.err = Describe(key, schema, options)
	return result
}
func (m Message[A]) Validate() error {
	if m.err != nil {
		return m.err
	}
	if err := m.arguments.Validate(); err != nil {
		return err
	}
	return m.definition.Validate()
}
func (m Message[A]) Key() i18n.MessageKey { return m.definition.Key }
func (m Message[A]) Definition() (i18n.MessageDefinition, error) {
	if err := m.Validate(); err != nil {
		return i18n.MessageDefinition{}, err
	}
	d := m.definition
	d.Parameters = slices.Clone(d.Parameters)
	return d, nil
}

type Description struct {
	Message   i18n.MessageDefinition `json:"message"`
	Arguments contract.Schema        `json:"arguments"`
}

func (m Message[A]) Description() (Description, error) {
	definition, err := m.Definition()
	if err != nil {
		return Description{}, err
	}
	schema, err := m.arguments.Description()
	return Description{definition, schema}, err
}

// Registration erases only the argument type at catalog assembly, not Format.
type Registration struct {
	definition func() (i18n.MessageDefinition, error)
}

func (m Message[A]) Registration() Registration { return Registration{m.Definition} }
func Definitions(registrations ...Registration) ([]i18n.MessageDefinition, error) {
	if len(registrations) > i18n.MaxMessages {
		return nil, invalid()
	}
	result := make([]i18n.MessageDefinition, 0, len(registrations))
	for _, registration := range registrations {
		if registration.definition == nil {
			return nil, invalid()
		}
		definition, err := registration.definition()
		if err != nil {
			return nil, err
		}
		result = append(result, definition)
	}
	return result, nil
}
func Load(ctx context.Context, source fs.FS, locales i18n.LocaleCatalog, options i18n.CatalogOptions, registrations ...Registration) (*i18n.Catalog, error) {
	definitions, err := Definitions(registrations...)
	if err != nil {
		return nil, err
	}
	return i18n.Load(ctx, source, locales, options, definitions...)
}

func (m Message[A]) Format(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID, args A) (i18n.Result, error) {
	if err := m.Validate(); err != nil {
		return i18n.Result{}, err
	}
	if ctx == nil || catalog.Accepts(m.definition) != nil {
		return i18n.Result{}, invalid()
	}
	locales, err := catalog.Snapshot(ctx)
	if err != nil {
		return i18n.Result{}, err
	}
	if !locales.Contains(locale) {
		return i18n.Result{}, invalid()
	}
	values, err := m.bindArguments(ctx, args)
	if err != nil {
		return i18n.Result{}, err
	}
	return catalog.FormatDynamic(ctx, locale, m.Key(), values)
}

// Bind snapshots typed declaration arguments once and validates an English
// fallback. Returned messages can be shared without retaining the argument object.
func (m Message[A]) Bind(ctx context.Context, args A, fallback i18n.Template) (i18n.PreparedMessage, error) {
	if err := m.Validate(); err != nil {
		return i18n.PreparedMessage{}, err
	}
	if ctx == nil {
		return i18n.PreparedMessage{}, invalid()
	}
	values, err := m.bindArguments(ctx, args)
	if err != nil {
		return i18n.PreparedMessage{}, err
	}
	return i18n.PrepareMessage(m.definition, values, fallback)
}

// BindLiteral binds typed arguments while retaining a literal fallback.
func (m Message[A]) BindLiteral(ctx context.Context, args A, text string) (i18n.PreparedMessage, error) {
	if err := m.Validate(); err != nil {
		return i18n.PreparedMessage{}, err
	}
	if ctx == nil {
		return i18n.PreparedMessage{}, invalid()
	}
	values, err := m.bindArguments(ctx, args)
	if err != nil {
		return i18n.PreparedMessage{}, err
	}
	return i18n.PrepareLiteralMessage(m.definition, values, text)
}

func (m Message[A]) bindArguments(ctx context.Context, args A) (map[string]i18n.Argument, error) {
	limits := contract.JSONLimits{Bytes: i18n.MaxTextBytes, Depth: 8, Nodes: 512, Steps: 1024, Issues: 1}
	encoded, err := m.arguments.Encode(ctx, args, limits)
	if err != nil {
		return nil, err
	}
	node, err := jsonwire.Decode(encoded, jsonwire.Limits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes})
	if err != nil {
		return nil, err
	}
	object, ok := node.(map[string]any)
	if !ok {
		return nil, invalid()
	}
	values := make(map[string]i18n.Argument, len(m.definition.Parameters))
	for _, parameter := range m.definition.Parameters {
		value := object[parameter.Name]
		info, err := m.arguments.DescribeScalarProperty(parameter.Name)
		if err != nil {
			return nil, err
		}
		if info.Quoted {
			text, ok := value.(string)
			if !ok {
				return nil, invalid()
			}
			if info.Value.Kind == contract.StringKind {
				var decoded string
				if json.Unmarshal([]byte(text), &decoded) != nil {
					return nil, invalid()
				}
				value = decoded
			}
		}
		switch parameter.Kind {
		case i18n.TextParameter:
			text, ok := value.(string)
			if !ok {
				return nil, invalid()
			}
			values[parameter.Name] = i18n.Text(text)
		case i18n.BooleanParameter:
			boolean, ok := value.(bool)
			if !ok {
				if text, stringValue := value.(string); info.Quoted && stringValue {
					boolean, err = strconv.ParseBool(text)
					ok = err == nil
				}
			}
			if !ok {
				return nil, invalid()
			}
			values[parameter.Name] = i18n.Boolean(boolean)
		case i18n.NumberParameter:
			text, ok := value.(string)
			if !ok {
				number, numeric := value.(json.Number)
				if !numeric {
					return nil, invalid()
				}
				text = string(number)
			}
			number, err := decimal.Parse(text)
			if err != nil {
				return nil, invalid()
			}
			values[parameter.Name] = i18n.Number(number)
		}
	}
	return values, nil
}
func invalid() error { return fault.New(fault.Invalid, "invalid typed localization message") }
