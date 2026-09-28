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
	if d.Key.Validate() != nil || len(d.Parameters) > MaxParameters {
		return invalidMessage()
	}
	seen := make(map[string]ParameterKind, len(d.Parameters))
	for _, p := range d.Parameters {
		if !ParameterName(p.Name) || seen[p.Name] != "" {
			return invalidMessage()
		}
		switch p.Kind {
		case TextParameter, NumberParameter, BooleanParameter:
		default:
			return invalidMessage()
		}
		seen[p.Name] = p.Kind
	}
	if d.Plural == "" {
		if d.Kind != "" {
			return invalidMessage()
		}
	} else if seen[d.Plural] != NumberParameter || d.Kind != Cardinal && d.Kind != Ordinal {
		return invalidMessage()
	}
	return nil
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
