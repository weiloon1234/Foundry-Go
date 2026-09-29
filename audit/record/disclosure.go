// Package record supplies typed audit snapshots and generated registration
// contracts below audit storage. Capturing a record performs no database I/O.
package record

import (
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Disclosure controls one generated model field's audit representation.
// Automatic includes ordinary fields and redacts sensitive codecs or conventional
// sensitive names.
// Exclude omits the entire field; Redact retains change flags without its value.
// There is deliberately no option to override sensitive-value/name protection.
type Disclosure uint8

const (
	Automatic Disclosure = iota
	Exclude
	Redact
)

func (d Disclosure) Validate() error {
	if d > Redact {
		return fault.New(fault.Invalid, "invalid audit field disclosure")
	}
	return nil
}

// Redaction identifies the sensitive-name policy that captured a stored audit
// row. Every row retains the policy that wrote it, so extending the recognized
// names never invalidates older history; reads check each row against its own
// policy. New captures always use CurrentRedaction.
type Redaction uint8

const (
	// RedactionV1 recognizes password, secret, token, credential, authorization
	// and API/private-key names split at snake, kebab and camel-case boundaries.
	RedactionV1 Redaction = 1
	// RedactionV2 additionally splits letter/digit boundaries, ignores digit
	// suffixes and simple plurals, recognizes joined names such as accesstoken,
	// and adds passphrase, OTP, PIN, CVV/CVC, SSN, card number, cookie, session
	// and recovery-code conventions.
	RedactionV2 Redaction = 2
	// CurrentRedaction is the policy applied to newly captured audit data.
	CurrentRedaction = RedactionV2
)

func (r Redaction) Validate() error {
	if r < RedactionV1 || r > CurrentRedaction {
		return fault.New(fault.Invalid, "unknown audit redaction policy")
	}
	return nil
}

// SensitiveName is the current shared rule for stored columns and nested JSON
// keys. Explicit exclusions are still required for secrets whose names do not
// identify their meaning.
func SensitiveName(name string) bool { return CurrentRedaction.Sensitive(name) }

// Sensitive applies this policy version's naming rule. Unknown versions treat
// every name as sensitive rather than disclosing data under a guessed rule.
func (r Redaction) Sensitive(name string) bool {
	switch r {
	case RedactionV1:
		return sensitiveV1(name)
	case RedactionV2:
		return sensitiveV2(name)
	default:
		return true
	}
}

// sensitiveV1 is the frozen original rule. Do not extend it: rows captured
// under it are validated with exactly this behavior.
func sensitiveV1(name string) bool {
	runes := []rune(name)
	var normalized strings.Builder
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			normalized.WriteByte('_')
			continue
		}
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) ||
			(i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsLower(runes[i+1]))) {
			normalized.WriteByte('_')
		}
		normalized.WriteRune(unicode.ToLower(r))
	}
	parts := strings.FieldsFunc(normalized.String(), func(r rune) bool { return r == '_' })
	for i, part := range parts {
		switch part {
		case "password", "passwd", "secret", "token", "credential", "credentials", "authorization", "apikey", "privatekey":
			return true
		case "api", "private":
			if i+1 < len(parts) && parts[i+1] == "key" {
				return true
			}
		}
	}
	return false
}

// sensitiveWords are complete words recognized by RedactionV2 after singular
// normalization. Short abbreviations only match as whole words.
var sensitiveWords = map[string]struct{}{
	"password": {}, "passwd": {}, "passphrase": {}, "secret": {}, "token": {}, "credential": {},
	"authorization": {}, "apikey": {}, "privatekey": {}, "otp": {}, "pin": {}, "cvv": {}, "cvc": {},
	"ssn": {}, "cardnumber": {}, "cookie": {}, "session": {}, "recoverycode": {},
}

// sensitiveSuffixes also match joined lowercase names such as accesstoken or
// userpassword. Only long, unambiguous words participate.
var sensitiveSuffixes = []string{"password", "passwd", "passphrase", "secret", "token", "credential",
	"authorization", "apikey", "privatekey", "cardnumber", "recoverycode"}

// sensitivePairs are two-word conventions such as api_key or recovery-codes.
var sensitivePairs = map[[2]string]struct{}{
	{"api", "key"}: {}, {"private", "key"}: {}, {"card", "number"}: {}, {"recovery", "code"}: {},
}

func sensitiveV2(name string) bool {
	words := nameWords(name)
	for i, word := range words {
		for _, candidate := range singulars(word) {
			if _, ok := sensitiveWords[candidate]; ok {
				return true
			}
			for _, suffix := range sensitiveSuffixes {
				if strings.HasSuffix(candidate, suffix) {
					return true
				}
			}
			if i+1 < len(words) {
				for _, next := range singulars(words[i+1]) {
					if _, ok := sensitivePairs[[2]string{candidate, next}]; ok {
						return true
					}
				}
			}
		}
	}
	return false
}

// nameWords splits snake, kebab, dotted and camel-case names, including acronym
// and letter/digit boundaries. Digit runs are dropped so token2 matches token.
func nameWords(name string) []string {
	runes := []rune(name)
	words := make([]string, 0, 4)
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for i, r := range runes {
		switch {
		case unicode.IsDigit(r):
			flush()
			continue
		case !unicode.IsLetter(r):
			flush()
			continue
		}
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) ||
			(i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsLower(runes[i+1]) && !pluralSuffix(runes, i+1))) {
			flush()
		}
		current.WriteRune(unicode.ToLower(r))
	}
	flush()
	return words
}

// pluralSuffix reports a lone lowercase "s" ending an acronym, as in PINs or
// IDs, which belongs to the acronym instead of starting a new word.
func pluralSuffix(runes []rune, at int) bool {
	return runes[at] == 's' && (at+1 == len(runes) || !unicode.IsLower(runes[at+1]))
}

// singulars returns a word with simple English plural endings removed. It is a
// conservative matching aid, not a grammar: tokens → token, cookies → cookie.
func singulars(word string) []string {
	result := []string{word}
	if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
		result = append(result, word[:len(word)-1])
		if len(word) > 4 && strings.HasSuffix(word, "es") {
			result = append(result, word[:len(word)-2])
		}
	}
	return result
}
