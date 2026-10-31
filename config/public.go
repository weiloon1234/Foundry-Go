package config

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// PublicKind classifies the browser validation and decoding applied to a public setting.
type PublicKind string

const (
	PublicString   PublicKind = "string"
	PublicBoolean  PublicKind = "boolean"
	PublicNumber   PublicKind = "number"
	PublicLocale   PublicKind = "locale"
	PublicCurrency PublicKind = "currency"
	PublicTimeZone PublicKind = "time_zone"
	PublicEnum     PublicKind = "enum"
)

func (k PublicKind) Validate() error {
	switch k {
	case "", PublicString, PublicBoolean, PublicNumber, PublicLocale, PublicCurrency, PublicTimeZone, PublicEnum:
		return nil
	default:
		return safeerror.NewFault(fault.Invalid, fmt.Sprintf("unsupported public config kind %q", string(k)))
	}
}

// PublicSetting is a browser-visible configuration setting declared in Go.
type PublicSetting struct {
	Name        string     `json:"name"`
	Env         string     `json:"env"`
	Description string     `json:"description,omitempty"`
	Fallback    string     `json:"fallback,omitzero"`
	Kind        PublicKind `json:"kind"`
	MaxLength   int        `json:"max_length,omitzero"`
	EnumCases   []string   `json:"enum_cases,omitempty"`
}
