// Package phone parses region-specific phone numbers into canonical E.164 values.
// Only importers of this package compile the phone numbering metadata.
package phone

import (
	"strings"

	"github.com/nyaruka/phonenumbers/v2"
	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// MaxInputBytes bounds the original input before trimming or parsing.
const MaxInputBytes = 100

// ExtensionPolicy controls extensions, which E.164 cannot represent.
type ExtensionPolicy uint8

const (
	// RejectExtensions rejects any parsed extension and is the default policy.
	RejectExtensions ExtensionPolicy = iota
	// DiscardExtensions explicitly permits losing the parsed extension.
	DiscardExtensions
)

// Options declares parsing policy. Its zero value rejects extensions.
type Options struct {
	// Extensions controls whether an extension rejects or is explicitly discarded.
	Extensions ExtensionPolicy
}

// Validate rejects unknown extension policies.
func (o Options) Validate() error {
	if o.Extensions != RejectExtensions && o.Extensions != DiscardExtensions {
		return safeerror.NewFault(fault.Invalid, "invalid phone extension policy")
	}
	return nil
}

// E164 is a validated canonical phone number. Construct it with Parse.
// The zero value is absent, and does not represent a valid phone number.
type E164 struct {
	text string
}

// String returns the canonical number, or empty text for the zero value.
func (n E164) String() string { return n.text }

// IsZero reports whether the number is absent.
func (n E164) IsZero() bool { return n.text == "" }

// Parse validates national or international input for the selected region and
// normalizes it to E.164. Empty input rejects. Optionality and allowed regions
// belong to the caller. Failures match fault.Invalid and never include input.
// Example: number, err := phone.Parse(countries.Code("MY"), "012-345 6789", phone.Options{}).
func Parse(region countries.Code, raw string, options Options) (E164, error) {
	if err := options.Validate(); err != nil {
		return E164{}, err
	}
	if len(raw) > MaxInputBytes || region.Validate() != nil {
		return E164{}, invalid()
	}
	number, err := phonenumbers.Parse(strings.TrimSpace(raw), string(region))
	if err != nil || options.Extensions == RejectExtensions && number.GetExtension() != "" || !phonenumbers.IsValidNumberForRegion(number, string(region)) {
		return E164{}, invalid()
	}
	return E164{text: phonenumbers.Format(number, phonenumbers.E164)}, nil
}

func invalid() error {
	return safeerror.NewFault(fault.Invalid, "phone number is invalid for the selected country")
}
