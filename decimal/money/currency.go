// Package money pairs exact decimal amounts with ISO 4217 currencies. Amounts
// never pass through floating point, arithmetic only combines one currency, and
// every rounding step names an explicit decimal.RoundingMode.
package money

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
	"golang.org/x/text/currency"
)

// Currency is an uppercase ISO 4217 alphabetic code such as "USD". A Go cast
// alone does not validate it; use ParseCurrency for external input.
type Currency string

// ParseCurrency accepts exactly three uppercase ASCII letters naming a known
// monetary currency. Known codes are the CLDR codes in golang.org/x/text plus
// newer ISO codes listed here. Precious metals, drawing rights, testing and
// "no currency" codes have no minor units and are rejected.
func ParseCurrency(code string) (Currency, error) {
	c := Currency(code)
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c, nil
}

func (c Currency) Validate() error {
	if len(c) != 3 {
		return invalidCurrency()
	}
	for i := range len(c) {
		if c[i] < 'A' || c[i] > 'Z' {
			return invalidCurrency()
		}
	}
	switch c {
	case "XAG", "XAU", "XBA", "XBB", "XBC", "XBD", "XDR", "XPD", "XPT", "XSU", "XTS", "XUA", "XXX":
		return invalidCurrency()
	case "MRU", "SLE", "UYW", "VED", "VES", "XCG", "ZWG":
		return nil
	}
	if _, err := currency.ParseISO(string(c)); err != nil {
		return invalidCurrency()
	}
	return nil
}

// MinorUnits reports the ISO 4217 number of fractional digits: 2 unless the
// currency is listed with 0, 3 or 4 minor units.
func (c Currency) MinorUnits() (int, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	switch c {
	case "BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "PYG", "RWF", "UGX", "UYI", "VND", "VUV", "XAF", "XOF", "XPF":
		return 0, nil
	case "BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND":
		return 3, nil
	case "CLF", "UYW":
		return 4, nil
	}
	return 2, nil
}

func (c Currency) String() string { return string(c) }

func (c Currency) MarshalText() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return []byte(c), nil
}

func (c *Currency) UnmarshalText(data []byte) error {
	parsed, err := ParseCurrency(string(data))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

func (c *Currency) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseCurrency)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

func invalidCurrency() error {
	return fault.New(fault.Invalid, "invalid or unsupported ISO 4217 currency code")
}
