package validation

import (
	"net"
	"strings"
	"unicode"
)

// Length requires exactly count Unicode code points.
func Length[S ~string](count int) Rule[S] { return LengthBetween[S](count, count) }

// LengthBetween combines inclusive text length bounds, rejecting reversed bounds.
func LengthBetween[S ~string](minimum, maximum int) Rule[S] {
	if minimum > maximum {
		return failed[S](invalid("text length bounds are reversed"))
	}
	return Bail(MinLength[S](minimum), MaxLength[S](maximum))
}

// DigitsBetween validates ASCII numeric text without discarding leading zeros.
func DigitsBetween[S ~string](minimum, maximum int) Rule[S] {
	return Bail(Digits[S](), LengthBetween[S](minimum, maximum))
}

// MinDigits combines digit-only validation with a minimum length.
func MinDigits[S ~string](minimum int) Rule[S] { return Bail(Digits[S](), MinLength[S](minimum)) }

// MaxDigits combines digit-only validation with a maximum length.
func MaxDigits[S ~string](maximum int) Rule[S] { return Bail(Digits[S](), MaxLength[S](maximum)) }

// ASCII creates a rule accepting only ASCII runes.
func ASCII[S ~string]() Rule[S] {
	return textRule[S]("foundry.ascii", true, func(v string) bool {
		return everyRune(v, func(r rune) bool { return r <= unicode.MaxASCII })
	})
}

// AlphaDash creates a rule accepting alphabetic runes, Unicode numbers, hyphens, and underscores.
func AlphaDash[S ~string]() Rule[S] {
	return textRule[S]("foundry.alpha_dash", true, func(v string) bool {
		return everyRune(v, func(r rune) bool { return alphabetic(r) || unicode.IsNumber(r) || r == '-' || r == '_' })
	})
}

// Lowercase and Uppercase check case without changing the received text.
// Uncased characters pass; nonempty/letter requirements are separate rules.
func Lowercase[S ~string]() Rule[S] {
	return textRule[S]("foundry.lowercase", true, func(v string) bool { return v == strings.ToLower(v) })
}

// Uppercase creates a rule requiring text to equal its uppercase form.
func Uppercase[S ~string]() Rule[S] {
	return textRule[S]("foundry.uppercase", true, func(v string) bool { return v == strings.ToUpper(v) })
}

// Contains creates a rule requiring the supplied substring.
func Contains[S ~string](part S) Rule[S] {
	return serverAffix("foundry.contains", part, strings.Contains)
}

// DoesntContain creates a rule excluding the supplied substring.
func DoesntContain[S ~string](part S) Rule[S] {
	return serverAffix("foundry.doesnt_contain", part, func(v, p string) bool { return !strings.Contains(v, p) })
}

// DoesntStartWith creates a rule excluding the supplied prefix.
func DoesntStartWith[S ~string](prefix S) Rule[S] {
	return serverAffix("foundry.doesnt_start_with", prefix, func(v, p string) bool { return !strings.HasPrefix(v, p) })
}

// DoesntEndWith creates a rule excluding the supplied suffix.
func DoesntEndWith[S ~string](suffix S) Rule[S] {
	return serverAffix("foundry.doesnt_end_with", suffix, func(v, p string) bool { return !strings.HasSuffix(v, p) })
}
func serverAffix[S ~string](id RuleID, part S, check func(string, string) bool) Rule[S] {
	rule := affixRule(id, part, check)
	rule.info.ServerOnly = true
	return rule
}

// HexColor accepts CSS hexadecimal colors (#RGB, #RGBA, #RRGGBB, #RRGGBBAA).
func HexColor[S ~string]() Rule[S] {
	return textRule[S]("foundry.hex_color", true, func(v string) bool {
		if len(v) != 4 && len(v) != 5 && len(v) != 7 && len(v) != 9 {
			return false
		}
		return v[0] == '#' && everyRune(v[1:], func(r rune) bool { return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' })
	})
}

// MACAddress accepts a six-byte address in formats supported by net.ParseMAC.
func MACAddress[S ~string]() Rule[S] {
	return textRule[S]("foundry.mac_address", true, func(v string) bool { address, err := net.ParseMAC(v); return err == nil && len(address) == 6 })
}

// ULID checks the 26-character Crockford encoding, including its 128-bit bound.
// Lowercase input is accepted. It does not require a particular timestamp.
func ULID[S ~string]() Rule[S] {
	return textRule[S]("foundry.ulid", true, func(v string) bool {
		if len(v) != 26 || v[0] < '0' || v[0] > '7' {
			return false
		}
		return everyRune(strings.ToUpper(v), func(r rune) bool { return strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) })
	})
}
