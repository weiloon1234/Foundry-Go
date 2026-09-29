package i18n

import (
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Template is either plain text or a plural form set. A plural template must
// include Other, and its registered message must declare the numeric parameter.
// Text and Forms cannot be combined. Catalog construction copies all data.
type Template struct {
	Text  string
	Forms map[PluralForm]string
}
type PluralForm string

const (
	Other PluralForm = "other"
	Zero  PluralForm = "zero"
	One   PluralForm = "one"
	Two   PluralForm = "two"
	Few   PluralForm = "few"
	Many  PluralForm = "many"
)

type templatePart struct{ text, parameter string }
type compiledTemplate map[PluralForm][]templatePart

// templateProblem is a developer-facing reason naming no template text or
// argument values. Catalog construction adds locale, file and key coordinates.
type templateProblem string

func compileTemplate(t Template, d MessageDefinition) (compiledTemplate, int, templateProblem) {
	forms := t.Forms
	if d.Plural == "" {
		if len(forms) != 0 {
			return nil, 0, "a nonplural message cannot declare plural forms"
		}
		forms = map[PluralForm]string{Other: t.Text}
	} else {
		if t.Text != "" || len(forms) == 0 || len(forms) > 6 {
			return nil, 0, "a plural message requires only $plural forms"
		}
		if _, ok := forms[Other]; !ok {
			return nil, 0, "a plural message requires the other form"
		}
	}
	parameters := make(map[string]bool, len(d.Parameters))
	for _, p := range d.Parameters {
		parameters[p.Name] = true
	}
	result := make(compiledTemplate, len(forms))
	size := 0
	for form, text := range forms {
		switch form {
		case Other, Zero, One, Two, Few, Many:
		default:
			return nil, 0, templateProblem("unsupported plural form " + boundedQuote(string(form)))
		}
		parts, problem := parseTemplate(text, parameters)
		if problem != "" {
			return nil, 0, problem
		}
		size += len(text)
		result[form] = parts
	}
	return result, size, ""
}

func parseTemplate(text string, parameters map[string]bool) ([]templatePart, templateProblem) {
	if !validText(text) {
		return nil, "template text is invalid UTF-8, contains NUL or exceeds 64 KiB"
	}
	var result []templatePart
	for len(text) > 0 {
		start := strings.Index(text, "{{")
		if start < 0 {
			if strings.Contains(text, "}}") {
				return nil, "template contains an unmatched }}"
			}
			result = append(result, templatePart{text: text})
			break
		}
		if strings.Contains(text[:start], "}}") {
			return nil, "template contains an unmatched }}"
		}
		if start > 0 {
			result = append(result, templatePart{text: text[:start]})
		}
		text = text[start+2:]
		end := strings.Index(text, "}}")
		if end < 0 {
			return nil, "template contains an unterminated {{ placeholder"
		}
		if !parameters[text[:end]] {
			return nil, templateProblem("placeholder " + boundedQuote(text[:end]) + " is not a declared parameter")
		}
		result = append(result, templatePart{parameter: text[:end]})
		text = text[end+2:]
	}
	return result, ""
}

// Decimal operands use their canonical numeric scale, matching decimal.Decimal;
// 1.0 and 1 are the same argument. No floating point conversion occurs.
// Tags are parsed once per catalog locale (see Catalog.tags) rather than per call.
func pluralForm(tag language.Tag, kind PluralKind, number string) PluralForm {
	number = strings.TrimPrefix(number, "-")
	whole, fraction, _ := strings.Cut(number, ".")
	rules := plural.Cardinal
	if kind == Ordinal {
		rules = plural.Ordinal
	}
	i, f, scale := pluralOperand(whole), pluralOperand(fraction), len(fraction)
	switch rules.MatchPlural(tag, i, scale, scale, f, f) {
	case plural.Zero:
		return Zero
	case plural.One:
		return One
	case plural.Two:
		return Two
	case plural.Few:
		return Few
	case plural.Many:
		return Many
	default:
		return Other
	}
}

// MatchPlural accepts large operands modulo 10,000,000. Keep an additional
// multiple when reducing a large value so literal equality rules cannot mistake
// it for a small number (or zero). This preserves all supported modulo rules and
// avoids MatchDigits' saturation of large integers to 1,000,000. Canonical decimal
// fractions have no trailing zeros, so v == w and f == t.
func pluralOperand(digits string) int {
	const modulus = 10_000_000
	digits = strings.TrimLeft(digits, "0")
	base := 0
	if len(digits) > 7 {
		base = modulus
		digits = digits[len(digits)-7:]
	}
	value := 0
	for i := range len(digits) {
		value = value*10 + int(digits[i]-'0')
	}
	return base + value
}

func renderTemplate(parts []templatePart, args map[string]Argument) (string, error) {
	if len(parts) == 1 && parts[0].parameter == "" {
		return parts[0].text, nil
	}
	size := 0
	for _, part := range parts {
		if part.parameter != "" {
			size += len(args[part.parameter].text)
		} else {
			size += len(part.text)
		}
	}
	var out strings.Builder
	out.Grow(min(size, MaxTextBytes))
	for _, part := range parts {
		text := part.text
		if part.parameter != "" {
			text = args[part.parameter].text
		}
		if len(text) > MaxTextBytes-out.Len() {
			return "", invalidMessage()
		}
		out.WriteString(text)
	}
	return out.String(), nil
}
