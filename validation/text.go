package validation

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

func parameter(name string, input any) Parameter {
	// Built-in constructors supply only native scalars, never application codecs.
	data, err := json.Marshal(input)
	if err != nil {
		return Parameter{Name: name}
	}
	return Parameter{Name: name, Value: data}
}

func textValue(s *execution, input string) bool {
	if len(input) > s.limits.ValueBytes {
		s.err = &LimitError{}
		return false
	}
	return utf8.ValidString(input)
}

// NonBlank rejects empty or whitespace-only text. It leaves the value intact;
// normalization belongs to explicit domain behavior or model mutators.
func NonBlank[S ~string]() Rule[S] {
	return valueRule(Spec{ID: "foundry.non_blank"}, false, func(s *execution, input S) (bool, error) {
		text := string(input)
		return textValue(s, text) && strings.TrimSpace(text) != "", nil
	})
}

// MinLength measures Unicode code points, not bytes or grapheme clusters.
func MinLength[S ~string](minimum int) Rule[S] {
	if minimum < 0 {
		return failed[S](invalid("minimum text length must not be negative"))
	}
	return valueRule(Spec{ID: "foundry.min_length", Parameters: []Parameter{parameter("min", minimum)}}, false, func(s *execution, input S) (bool, error) {
		text := string(input)
		return textValue(s, text) && utf8.RuneCountInString(text) >= minimum, nil
	})
}

func MaxLength[S ~string](maximum int) Rule[S] {
	if maximum < 0 {
		return failed[S](invalid("maximum text length must not be negative"))
	}
	return valueRule(Spec{ID: "foundry.max_length", Parameters: []Parameter{parameter("max", maximum)}}, false, func(s *execution, input S) (bool, error) {
		text := string(input)
		return textValue(s, text) && utf8.RuneCountInString(text) <= maximum, nil
	})
}

// Matches uses Go's regexp semantics. It is server-only metadata because a
// JavaScript regular expression does not promise identical syntax or behavior.
func Matches[S ~string](pattern string) Rule[S] { return patternRule[S](pattern, true) }

// NotMatches rejects text matching a Go regular expression.
func NotMatches[S ~string](pattern string) Rule[S] { return patternRule[S](pattern, false) }

func patternRule[S ~string](pattern string, match bool) Rule[S] {
	if !validText(pattern, true) {
		return failed[S](invalid("invalid validation pattern"))
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return failed[S](invalid("invalid validation regular expression"))
	}
	id := RuleID("foundry.matches")
	if !match {
		id = "foundry.not_matches"
	}
	return valueRule(Spec{ID: id, Parameters: []Parameter{parameter("pattern", pattern)}}, true, func(s *execution, input S) (bool, error) {
		text := string(input)
		return textValue(s, text) && compiled.MatchString(text) == match, nil
	})
}
