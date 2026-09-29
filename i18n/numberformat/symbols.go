package numberformat

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/decimal/money"
	"golang.org/x/text/currency"
	"golang.org/x/text/message"
	xnumber "golang.org/x/text/number"
)

type affix struct{ prefix, suffix string }

// symbols is the locale's number presentation, derived once per Formatter.
type symbols struct {
	digits                   [10]string
	decimal, group           string
	primary, secondary       int
	positive, negative       affix
	percent, negativePercent affix
}

// derive formats exact integer probes with x/text and reads the locale's
// digits, separators, grouping and sign/percent affixes from the output. Only
// integers are formatted, so no floating-point value is involved.
func derive(printer *message.Printer) (symbols, error) {
	var s symbols
	digitRunes := []rune(stripFormatting(printer.Sprint(xnumber.Decimal(1234567890, xnumber.NoSeparator()))))
	if len(digitRunes) != 10 {
		return symbols{}, underivable()
	}
	for i, r := range digitRunes {
		if !unicode.IsDigit(r) {
			return symbols{}, underivable()
		}
		s.digits[(i+1)%10] = string(r)
	}
	if err := s.deriveSeparators(printer.Sprint(xnumber.Decimal(1234567, xnumber.Scale(1)))); err != nil {
		return symbols{}, err
	}
	var err error
	if s.positive, err = s.affixAround(printer.Sprint(xnumber.Decimal(1)), "1"); err != nil {
		return symbols{}, err
	}
	if s.negative, err = s.affixAround(printer.Sprint(xnumber.Decimal(-1)), "1"); err != nil {
		return symbols{}, err
	}
	if s.percent, err = s.affixAround(printer.Sprint(xnumber.Percent(1)), "100"); err != nil {
		return symbols{}, err
	}
	if s.negativePercent, err = s.affixAround(printer.Sprint(xnumber.Percent(-1)), "100"); err != nil {
		return symbols{}, err
	}
	return s, nil
}

// token is one run of locale digits (as ASCII) or of other text.
type token struct {
	text  string
	digit bool
}

func (s *symbols) tokens(text string) []token {
	var result []token
	for len(text) > 0 {
		digit, size := s.digitAt(text)
		isDigit := size > 0
		if !isDigit {
			_, size = utf8.DecodeRuneInString(text)
		}
		value := text[:size]
		if isDigit {
			value = string(rune('0' + digit))
		}
		if n := len(result); n > 0 && result[n-1].digit == isDigit {
			result[n-1].text += value
		} else {
			result = append(result, token{text: value, digit: isDigit})
		}
		text = text[size:]
	}
	return result
}

func (s *symbols) digitAt(text string) (int, int) {
	for digit, symbol := range s.digits {
		if strings.HasPrefix(text, symbol) {
			return digit, len(symbol)
		}
	}
	return 0, 0
}

// deriveSeparators reads "1,234,567.0"-shaped output. Integer group lengths
// give the primary (rightmost) and secondary grouping sizes.
func (s *symbols) deriveSeparators(text string) error {
	tokens := s.tokens(text)
	if len(tokens) < 3 || !tokens[0].digit || !tokens[len(tokens)-1].digit || tokens[len(tokens)-1].text != "0" {
		return underivable()
	}
	var groups []string
	var digits strings.Builder
	for i, t := range tokens {
		if t.digit != (i%2 == 0) {
			return underivable()
		}
		if t.digit {
			digits.WriteString(t.text)
			if i < len(tokens)-1 {
				groups = append(groups, t.text)
			}
		} else if i < len(tokens)-2 {
			if s.group == "" {
				s.group = t.text
			} else if s.group != t.text {
				return underivable()
			}
		}
	}
	if digits.String() != "12345670" {
		return underivable()
	}
	s.decimal = tokens[len(tokens)-2].text
	if len(groups) > 1 {
		s.primary = len(groups[len(groups)-1])
		s.secondary = s.primary
		if len(groups) > 2 {
			s.secondary = len(groups[len(groups)-2])
		}
		if s.primary == 0 || s.secondary == 0 {
			return underivable()
		}
	}
	return nil
}

// affixAround splits output around its single digit run, which must be digits.
func (s *symbols) affixAround(text, digits string) (affix, error) {
	tokens := s.tokens(text)
	index := slices.IndexFunc(tokens, func(t token) bool { return t.digit })
	if index < 0 || tokens[index].text != digits || slices.ContainsFunc(tokens[index+1:], func(t token) bool { return t.digit }) {
		return affix{}, underivable()
	}
	var result affix
	for _, t := range tokens[:index] {
		result.prefix += t.text
	}
	for _, t := range tokens[index+1:] {
		result.suffix += t.text
	}
	return result, nil
}

// stripFormatting removes invisible bidi/format controls from a probe.
func stripFormatting(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
}

func underivable() error { return invalid("locale number symbols could not be derived") }

// currencySymbol resolves a locale symbol through x/text. Codes newer than its
// CLDR data, and the Code display, use the ISO code itself.
func (f *Formatter) currencySymbol(code money.Currency, display CurrencyDisplay) string {
	if display == Code {
		return string(code)
	}
	unit, err := currency.ParseISO(string(code))
	if err != nil || unit.String() != string(code) {
		return string(code)
	}
	if display == NarrowSymbol {
		return f.printer.Sprint(currency.NarrowSymbol(unit))
	}
	return f.printer.Sprint(currency.Symbol(unit))
}

// needsCurrencySpace applies CLDR currency spacing: a symbol whose edge next
// to the digits is neither a symbol nor a separator, such as the letter in
// "CHF", is followed by a no-break space.
func needsCurrencySpace(edge rune) bool {
	return edge != utf8.RuneError && !unicode.IsSymbol(edge) && !unicode.In(edge, unicode.Z)
}

func lastRune(text string) rune {
	r, _ := utf8.DecodeLastRuneInString(text)
	return r
}
