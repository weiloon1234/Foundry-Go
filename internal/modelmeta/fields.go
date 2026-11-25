// Package modelmeta shares field naming and tag parsing between declarations and generation.
package modelmeta

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"strconv"
	"strings"
	"unicode"
)

// ParseTags rejects malformed and repeated struct tags. Example: ParseTags(`foundry:"-"`).
func ParseTags(text string) (map[string]string, error) {
	tags := make(map[string]string)
	for text != "" {
		text = strings.TrimLeft(text, " ")
		if text == "" {
			break
		}
		colon := strings.IndexByte(text, ':')
		if colon < 1 {
			return nil, safeerror.New("malformed struct tag; write key:\"value\" pairs separated by spaces")
		}
		key := text[:colon]
		for _, r := range key {
			if r <= ' ' || r == '"' || r == 127 {
				return nil, safeerror.New("malformed struct tag key; use a key without spaces or quotes")
			}
		}
		remaining := text[colon+1:]
		if len(remaining) == 0 || remaining[0] != '"' {
			return nil, safeerror.New("struct tag values must be quoted; write key:\"value\"")
		}
		quoted, err := strconv.QuotedPrefix(remaining)
		if err != nil {
			return nil, safeerror.New("malformed quoted struct tag; close the quote")
		}
		value, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, safeerror.New("invalid struct tag value; use a valid quoted string")
		}
		if _, exists := tags[key]; exists {
			return nil, safeerror.New(fmt.Sprintf("duplicate struct tag namespace %s; declare it once", key))
		}
		tags[key] = value
		text = remaining[len(quoted):]
		if text != "" && text[0] != ' ' {
			return nil, safeerror.New(fmt.Sprintf("struct tags must be separated by spaces"))
		}
	}
	return tags, nil
}

// Snake preserves the generator's column naming convention. Example: Snake("BuyerID").
func Snake(name string) string {
	runes := []rune(name)
	var out strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(runes[i-1]))) {
			out.WriteByte('_')
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}
