package validation

import (
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

// StartsWith checks an exact, case-sensitive prefix without normalization.
// An empty prefix matches every string; use NonBlank to require nonempty text.
func StartsWith[S ~string](prefix S) Rule[S] {
	return affixRule("foundry.starts_with", prefix, strings.HasPrefix)
}

// EndsWith checks an exact, case-sensitive suffix without normalization.
func EndsWith[S ~string](suffix S) Rule[S] {
	return affixRule("foundry.ends_with", suffix, strings.HasSuffix)
}

func affixRule[S ~string](id RuleID, affix S, check func(string, string) bool) Rule[S] {
	text := string(affix)
	if !validText(text, true) {
		return failed[S](invalid("invalid validation text affix"))
	}
	return valueRule(Spec{ID: id, Parameters: []Parameter{parameter("value", text)}}, false, func(s *execution, input S) (bool, error) {
		return textValue(s, string(input)) && check(string(input), text), nil
	})
}

func alphabetic(r rune) bool {
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_Alphabetic, r)
}

func everyRune(text string, check func(rune) bool) bool {
	for _, r := range text {
		if !check(r) {
			return false
		}
	}
	return true
}

// Alpha accepts Unicode Alphabetic characters, including alphabetic marks.
// Empty text passes; use NonBlank for presence. Unicode-table behavior is
// server-only metadata because browser Unicode versions can differ.
func Alpha[S ~string]() Rule[S] {
	return textRule[S]("foundry.alpha", true, func(text string) bool { return everyRune(text, alphabetic) })
}

// AlphaNumeric accepts Unicode Alphabetic and Number characters. It does not
// accept punctuation or normalize combining marks, case or scripts.
func AlphaNumeric[S ~string]() Rule[S] {
	return textRule[S]("foundry.alpha_numeric", true, func(text string) bool {
		return everyRune(text, func(r rune) bool { return alphabetic(r) || unicode.IsNumber(r) })
	})
}

// Digits accepts only ASCII digits, preserving leading zeros. Empty text passes;
// use NonBlank and length rules when the field must contain a fixed-size code.
func Digits[S ~string]() Rule[S] {
	return textRule[S]("foundry.digits", false, func(text string) bool {
		return everyRune(text, func(r rune) bool { return r >= '0' && r <= '9' })
	})
}

// Timezone accepts UTC, an installed IANA zone or a fixed +/-HH:MM offset.
// It rejects implicit machine-local zones and retains server-only metadata.
func Timezone[S ~string]() Rule[S] {
	return textRule[S]("foundry.timezone", true, func(text string) bool { _, err := temporal.ParseTimeZone(text); return err == nil })
}
