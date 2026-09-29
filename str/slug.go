// Package str provides rune-safe string helpers: slugs, limits, masks,
// English inflection and human-readable file sizes. Functions are pure, never
// modify their input and are safe for concurrent use.
package str

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Slug returns a lowercase URL slug separated by "-". Latin letters with
// diacritics fold to ASCII ("Crème Brûlée" becomes "creme-brulee"), letters
// from other scripts are kept lowercase, whitespace, "-" and "_" separate
// words, and other punctuation and symbols are removed ("Don't stop!" becomes
// "dont-stop"). The result can be empty; validate it where one is required.
func Slug(text string) string {
	if !isASCII(text) {
		text = norm.NFKD.String(text)
	}
	var builder strings.Builder
	builder.Grow(len(text))
	pending := false
	separate := func() {
		if pending && builder.Len() > 0 {
			builder.WriteByte('-')
		}
		pending = false
	}
	for _, r := range text {
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		switch {
		case 'a' <= r && r <= 'z' || '0' <= r && r <= '9':
			separate()
			builder.WriteRune(r)
		case unicode.IsSpace(r) || r == '-' || r == '_':
			pending = true
		case r < utf8.RuneSelf || unicode.Is(unicode.Mn, r):
			// ASCII punctuation is removed; combining marks left by
			// decomposition belong to the previous letter.
		default:
			if ascii := folded(r); ascii != "" {
				separate()
				builder.WriteString(ascii)
			} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
				separate()
				builder.WriteRune(unicode.ToLower(r))
			}
		}
	}
	return builder.String()
}

// folded maps Latin letters that do not decompose to ASCII.
func folded(r rune) string {
	switch r {
	case 'ß':
		return "ss"
	case 'æ', 'Æ':
		return "ae"
	case 'œ', 'Œ':
		return "oe"
	case 'ø', 'Ø':
		return "o"
	case 'đ', 'Đ', 'ð', 'Ð':
		return "d"
	case 'ł', 'Ł':
		return "l"
	case 'þ', 'Þ':
		return "th"
	case 'ı':
		return "i"
	}
	return ""
}

func isASCII(text string) bool {
	for i := range len(text) {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
