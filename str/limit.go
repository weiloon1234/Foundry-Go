package str

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limit keeps at most limit runes of text. When text is shortened, trailing
// whitespace of the kept part is removed and suffix is appended, so the result
// can exceed limit by the suffix: Limit("The quick fox", 9, "...") is
// "The quick...". Text within the limit is returned unchanged.
func Limit(text string, limit int, suffix string) string {
	end, shortened := runePrefix(text, max(limit, 0))
	if !shortened {
		return text
	}
	return strings.TrimRightFunc(text[:end], unicode.IsSpace) + suffix
}

// Truncate shortens text so the result, including suffix, has at most width
// runes: Truncate("The quick fox", 8, "…") is "The qui…". When suffix alone
// does not fit, the first width runes are returned without it.
func Truncate(text string, width int, suffix string) string {
	width = max(width, 0)
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	keep := width - utf8.RuneCountInString(suffix)
	if keep <= 0 {
		end, _ := runePrefix(text, width)
		return text[:end]
	}
	return Limit(text, keep, suffix)
}

// runePrefix returns the byte length of the first n runes and whether text
// has more runes than n.
func runePrefix(text string, n int) (int, bool) {
	count := 0
	for i := range text {
		if count == n {
			return i, true
		}
		count++
	}
	return len(text), false
}

// Mask replaces length runes starting at rune position start with mask. A
// negative start counts from the end and a negative length masks through the
// end: Mask("taylor@example.com", '*', -15, 3) is "tay***@example.com".
// Positions outside text are ignored.
func Mask(text string, mask rune, start, length int) string {
	total := utf8.RuneCountInString(text)
	if start < 0 {
		start = max(total+start, 0)
	}
	end := total
	if length >= 0 && length < total-start {
		end = start + length
	}
	if start >= end {
		return text
	}
	startByte, _ := runePrefix(text, start)
	endByte, _ := runePrefix(text, end)
	replacement := string(mask)
	var builder strings.Builder
	builder.Grow(startByte + (end-start)*len(replacement) + len(text) - endByte)
	builder.WriteString(text[:startByte])
	for range end - start {
		builder.WriteString(replacement)
	}
	builder.WriteString(text[endByte:])
	return builder.String()
}
