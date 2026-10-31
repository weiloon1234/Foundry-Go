package config

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// PublicKind classifies the browser validation and decoding applied to a public setting.
type PublicKind string

const (
	PublicString       PublicKind = "string"
	PublicBoolean      PublicKind = "boolean"
	PublicNumber       PublicKind = "number"
	PublicLocale       PublicKind = "locale"
	PublicFormatLocale PublicKind = "format_locale"
	PublicCurrency     PublicKind = "currency"
	PublicTimeZone     PublicKind = "time_zone"
	PublicEnum         PublicKind = "enum"
)

func (k PublicKind) Validate() error {
	switch k {
	case "", PublicString, PublicBoolean, PublicNumber, PublicLocale, PublicFormatLocale, PublicCurrency, PublicTimeZone, PublicEnum:
		return nil
	default:
		return safeerror.NewFault(fault.Invalid, fmt.Sprintf("unsupported public config kind %q", string(k)))
	}
}

// PublicSetting is a browser-visible configuration setting declared in Go.
// Optional settings decode missing or blank browser values as undefined and cannot declare a non-empty fallback.
type PublicSetting struct {
	Name        string     `json:"name"`
	Env         string     `json:"env"`
	Description string     `json:"description,omitempty"`
	Fallback    string     `json:"fallback,omitzero"`
	Optional    bool       `json:"optional,omitempty"`
	Kind        PublicKind `json:"kind"`
	MaxLength   int        `json:"max_length,omitzero"`
	EnumCases   []string   `json:"enum_cases,omitempty"`
}
