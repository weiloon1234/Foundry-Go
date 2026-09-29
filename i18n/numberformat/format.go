// Package numberformat presents exact decimals, percentages and money for one
// locale. Digits, decimal/grouping separators, grouping sizes, signs, percent
// patterns and currency symbols come from the CLDR data in golang.org/x/text.
// Values stay exact decimal text; nothing is converted to floating point.
package numberformat

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/decimal/money"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Formatter is immutable after New and safe for concurrent use. Construct one
// per locale and retain it; construction derives the locale's symbols.
type Formatter struct {
	locale    i18n.LocaleID
	printer   *message.Printer
	symbols   symbols
	placement Placement
}

// New derives formatting symbols for a canonical locale ID, such as "en",
// "de-CH" or "ar-u-nu-latn". Locale membership is the caller's catalog policy.
func New(locale i18n.LocaleID) (*Formatter, error) {
	if err := locale.Validate(); err != nil {
		return nil, err
	}
	tag, err := language.Parse(string(locale))
	if err != nil {
		return nil, invalid("invalid number format locale")
	}
	printer := message.NewPrinter(tag)
	derived, err := derive(printer)
	if err != nil {
		return nil, err
	}
	return &Formatter{locale: locale, printer: printer, symbols: derived, placement: defaultPlacement(tag)}, nil
}

func (f *Formatter) Locale() i18n.LocaleID { return f.locale }

// Number formats the exact value with every canonical fractional digit and
// the locale's grouping, for example 1234567.5 as "1.234.567,5" in "de".
func (f *Formatter) Number(value decimal.Decimal) string {
	return f.signed(value.String(), f.symbols.positive, f.symbols.negative)
}

// Fixed rounds to scale fractional digits with mode and always shows exactly
// scale digits, for example 2.5 at scale 2 as "2.50".
func (f *Formatter) Fixed(value decimal.Decimal, scale int, mode decimal.RoundingMode) (string, error) {
	text, err := fixed(value, scale, mode)
	if err != nil {
		return "", err
	}
	return f.signed(text, f.symbols.positive, f.symbols.negative), nil
}

// Percent formats a ratio as a percentage: 0.125 at scale 1 is "12.5%" in
// "en" and "12,5 %" in "de". The ratio is multiplied by 100 exactly before
// rounding to scale with mode.
func (f *Formatter) Percent(ratio decimal.Decimal, scale int, mode decimal.RoundingMode) (string, error) {
	percent, err := ratio.Mul(decimal.FromInt64(100))
	if err != nil {
		return "", err
	}
	text, err := fixed(percent, scale, mode)
	if err != nil {
		return "", err
	}
	return f.signed(text, f.symbols.percent, f.symbols.negativePercent), nil
}

func fixed(value decimal.Decimal, scale int, mode decimal.RoundingMode) (string, error) {
	rounded, err := value.Round(scale, mode)
	if err != nil {
		return "", err
	}
	return rounded.Fixed(scale)
}

// signed renders exact decimal text ("-1234.50") inside sign affixes.
func (f *Formatter) signed(text string, positive, negative affix) string {
	wrap := positive
	if strings.HasPrefix(text, "-") {
		text, wrap = text[1:], negative
	}
	var builder strings.Builder
	builder.Grow(len(wrap.prefix) + len(wrap.suffix) + 2*len(text) + 8)
	builder.WriteString(wrap.prefix)
	f.digits(&builder, text)
	builder.WriteString(wrap.suffix)
	return builder.String()
}

// digits writes unsigned decimal text with locale digits, grouping and
// decimal separator.
func (f *Formatter) digits(builder *strings.Builder, text string) {
	whole, fraction, point := strings.Cut(text, ".")
	s := &f.symbols
	for i := range len(whole) {
		if i > 0 && s.primary > 0 && len(whole) > s.primary {
			if remaining := len(whole) - i; remaining == s.primary || remaining > s.primary && (remaining-s.primary)%s.secondary == 0 {
				builder.WriteString(s.group)
			}
		}
		builder.WriteString(s.digits[whole[i]-'0'])
	}
	if point {
		builder.WriteString(s.decimal)
		for i := range len(fraction) {
			builder.WriteString(s.digits[fraction[i]-'0'])
		}
	}
}

func invalid(message string) error { return fault.New(fault.Invalid, message) }

// Placement selects where a currency symbol appears relative to the number.
// The zero value is invalid; New selects the locale default.
type Placement uint8

const (
	// SymbolBefore renders "$1.00". A symbol ending in a letter, such as
	// "CHF", is separated by a no-break space, following CLDR currency spacing.
	SymbolBefore Placement = iota + 1
	// SymbolBeforeSpaced renders "€ 1,00" with a no-break space.
	SymbolBeforeSpaced
	// SymbolAfterSpaced renders "1,00 €" with a no-break space.
	SymbolAfterSpaced
)

func (p Placement) Validate() error {
	if p < SymbolBefore || p > SymbolAfterSpaced {
		return invalid("invalid currency placement")
	}
	return nil
}

// Placement reports the currency placement used by Money.
func (f *Formatter) Placement() Placement { return f.placement }

// WithCurrencyPlacement returns a copy that places currency symbols as p.
func (f *Formatter) WithCurrencyPlacement(p Placement) (*Formatter, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	copy := *f
	copy.placement = p
	return &copy, nil
}

// defaultPlacement follows CLDR standard currency patterns for the listed
// languages and regions. Other languages place the symbol first.
func defaultPlacement(tag language.Tag) Placement {
	base, _ := tag.Base()
	region, _ := tag.Region()
	switch base.String() + "-" + region.String() {
	case "de-AT", "de-CH", "de-LI", "it-CH":
		return SymbolBeforeSpaced
	case "pt-PT", "pt-AO", "pt-MZ":
		return SymbolAfterSpaced
	case "es-419", "es-MX", "es-US":
		return SymbolBefore
	}
	switch base.String() {
	case "ar", "be", "bg", "ca", "cs", "da", "de", "el", "es", "et", "fi", "fr", "he", "hr", "hu", "is", "it", "kk", "lt", "lv", "nb", "nn", "no", "pl", "ro", "ru", "sk", "sl", "sr", "sv", "uk", "vi":
		return SymbolAfterSpaced
	case "nl", "pt":
		return SymbolBeforeSpaced
	}
	return SymbolBefore
}

// CurrencyDisplay selects how Money names its currency. The zero value is
// invalid so each call states it.
type CurrencyDisplay uint8

const (
	// Symbol uses the locale's standard symbol, such as "US$" or "$".
	Symbol CurrencyDisplay = iota + 1
	// NarrowSymbol uses the shortest symbol, such as "$".
	NarrowSymbol
	// Code uses the ISO 4217 code, such as "USD".
	Code
)

func (d CurrencyDisplay) Validate() error {
	if d < Symbol || d > Code {
		return invalid("invalid currency display")
	}
	return nil
}

// Money formats an amount at its currency's minor units, for example
// 1234.5 USD as "$1,234.50" in "en" and "1.234,50 $" in "de". The minus sign
// precedes the whole currency pattern.
func (f *Formatter) Money(amount money.Money, display CurrencyDisplay) (string, error) {
	if err := display.Validate(); err != nil {
		return "", err
	}
	if err := amount.Validate(); err != nil {
		return "", err
	}
	minor, err := amount.Currency().MinorUnits()
	if err != nil {
		return "", err
	}
	text, err := amount.Amount().Fixed(minor)
	if err != nil {
		return "", err
	}
	wrap := f.symbols.positive
	if strings.HasPrefix(text, "-") {
		text, wrap = text[1:], f.symbols.negative
	}
	symbol := f.currencySymbol(amount.Currency(), display)
	var builder strings.Builder
	builder.Grow(len(wrap.prefix) + len(wrap.suffix) + len(symbol) + 2*len(text) + 12)
	builder.WriteString(wrap.prefix)
	switch f.placement {
	case SymbolBefore:
		builder.WriteString(symbol)
		if needsCurrencySpace(lastRune(symbol)) {
			builder.WriteString(noBreakSpace)
		}
		f.digits(&builder, text)
	case SymbolBeforeSpaced:
		builder.WriteString(symbol)
		builder.WriteString(noBreakSpace)
		f.digits(&builder, text)
	default:
		f.digits(&builder, text)
		builder.WriteString(noBreakSpace)
		builder.WriteString(symbol)
	}
	builder.WriteString(wrap.suffix)
	return builder.String(), nil
}

const noBreakSpace = " "
