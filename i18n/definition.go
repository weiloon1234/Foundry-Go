package i18n

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

const (
	MaxMessages     = 10000
	MaxParameters   = 64
	MaxTextBytes    = 64 << 10
	MaxCatalogBytes = 16 << 20
)

type ParameterKind string

const (
	TextParameter    ParameterKind = "text"
	NumberParameter  ParameterKind = "number"
	BooleanParameter ParameterKind = "boolean"
)

// Parameter describes a declared argument, never a received argument value.
type Parameter struct {
	Name string        `json:"name"`
	Kind ParameterKind `json:"kind"`
}

type PluralKind string

const (
	Cardinal PluralKind = "cardinal"
	Ordinal  PluralKind = "ordinal"
)

// MessageDefinition is the explicit dynamic declaration boundary. Generated
// typed messages derive it from their existing JSON contract. Plural names one
// numeric parameter; Kind is empty unless Plural is set.
type MessageDefinition struct {
	Key        MessageKey  `json:"key"`
	Parameters []Parameter `json:"parameters,omitempty"`
	Plural     string      `json:"plural,omitempty"`
	Kind       PluralKind  `json:"plural_kind,omitempty"`
}

func (d MessageDefinition) Validate() error {
	if problem := d.problem(); problem != "" {
		return definitionError(d.Key, problem)
	}
	return nil
}
func (d MessageDefinition) problem() string {
	if d.Key.Validate() != nil {
		return "the message key is not a semantic identifier"
	}
	if len(d.Parameters) > MaxParameters {
		return "the message declares more than 64 parameters"
	}
	var buffer [8]Parameter
	seen := buffer[:0]
	for _, p := range d.Parameters {
		if !ParameterName(p.Name) {
			return "parameter " + boundedQuote(p.Name) + " is not a valid name"
		}
		for _, earlier := range seen {
			if earlier.Name == p.Name {
				return "parameter " + boundedQuote(p.Name) + " is declared twice"
			}
		}
		switch p.Kind {
		case TextParameter, NumberParameter, BooleanParameter:
		default:
			return "parameter " + boundedQuote(p.Name) + " has an unsupported kind"
		}
		seen = append(seen, p)
	}
	if d.Plural == "" {
		if d.Kind != "" {
			return "a plural kind requires a plural parameter"
		}
		return ""
	}
	for _, p := range seen {
		if p.Name == d.Plural {
			if p.Kind != NumberParameter {
				return "plural parameter " + boundedQuote(d.Plural) + " must be a number"
			}
			if d.Kind != Cardinal && d.Kind != Ordinal {
				return "plural kind must be cardinal or ordinal"
			}
			return ""
		}
	}
	return "plural parameter " + boundedQuote(d.Plural) + " is not declared"
}

// ParameterName is shared by generated declarations and catalog placeholders.
func ParameterName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for i, b := range []byte(name) {
		if !(b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || i > 0 && b >= '0' && b <= '9') {
			return false
		}
	}
	return true
}

func cloneDefinition(d MessageDefinition) MessageDefinition {
	d.Parameters = slices.Clone(d.Parameters)
	return d
}
func sameDefinition(a, b MessageDefinition) bool {
	return a.Key == b.Key && a.Plural == b.Plural && a.Kind == b.Kind && slices.Equal(a.Parameters, b.Parameters)
}
func invalidMessage() error {
	return fault.New(fault.Invalid, "invalid localization declaration, catalog or argument")
}

// maxQuotedBytes bounds each developer-owned coordinate quoted in an error.
const maxQuotedBytes = 128

// boundedQuote quotes developer-owned identifiers (keys, files, parameter and
// locale names) as printable ASCII, truncating long input at a rune boundary.
func boundedQuote(text string) string {
	if len(text) <= maxQuotedBytes {
		return strconv.QuoteToASCII(text)
	}
	cut := maxQuotedBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strconv.QuoteToASCII(text[:cut]) + "..."
}

// catalogError names the locale, file and key of a catalog problem. Only these
// developer-owned coordinates and a fixed reason appear; template text, JSON
// values and argument values are never included. Empty coordinates are omitted.
func catalogError(locale LocaleID, file, key, reason string) error {
	var message strings.Builder
	message.WriteString("invalid localization catalog")
	separator := " ("
	for _, part := range [...]struct{ name, value string }{{"locale", string(locale)}, {"file", file}, {"key", key}} {
		if part.value == "" {
			continue
		}
		message.WriteString(separator)
		message.WriteString(part.name)
		message.WriteByte(' ')
		message.WriteString(boundedQuote(part.value))
		separator = ", "
	}
	if separator == ", " {
		message.WriteByte(')')
	}
	message.WriteString(": ")
	message.WriteString(reason)
	return fault.New(fault.Invalid, message.String())
}

// definitionError names the declaration key and problem; declarations are
// developer-owned and contain no received values.
func definitionError(key MessageKey, reason string) error {
	return fault.New(fault.Invalid, "invalid localization declaration "+boundedQuote(string(key))+": "+reason)
}

// argumentError names the message key and parameter, never the argument value.
func argumentError(key MessageKey, parameter, reason string) error {
	message := "invalid localization arguments for " + boundedQuote(string(key))
	if parameter != "" {
		message += " parameter " + boundedQuote(parameter)
	}
	return fault.New(fault.Invalid, message+": "+reason)
}
func validText(s string) bool {
	return len(s) <= MaxTextBytes && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// Argument is an explicit dynamic formatting value. Its zero value is invalid.
// Text is plain text; applications must escape the completed message for its
// output context. Values are substituted once and never interpreted as templates.
type Argument struct {
	kind ParameterKind
	text string
}

func Text(value string) Argument            { return Argument{TextParameter, value} }
func Number(value decimal.Decimal) Argument { return Argument{NumberParameter, value.String()} }
func Boolean(value bool) Argument           { return Argument{BooleanParameter, strconv.FormatBool(value)} }
func (a Argument) valid(kind ParameterKind) bool {
	return a.kind == kind && validText(a.text) && (kind != NumberParameter || len(a.text) <= decimal.MaxDigits+2)
}
