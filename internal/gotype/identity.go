// Package gotype owns canonical generated Go type identities. It describes
// names, never infers JSON fields or executes application codecs.
package gotype

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"
)

// Identity preserves historical composite IDs based on Go source type syntax.
// Named/scalar types retain readable qualified identities. Generic argument
// formatting is normalized without editing quoted struct tags.
func Identity(source string, named bool) string {
	if named {
		return Compact(source)
	}
	return fmt.Sprintf("go:%x", sha256.Sum256([]byte(source)))
}

// Compact removes formatting whitespace but retains field/type boundaries,
// including pointer and collection types. It equates go/types and native generic names, including anonymous
// struct arguments, without conflating a field name and its type.
func Compact(source string) string {
	var result strings.Builder
	quoted, escaped, pending := rune(0), false, false
	var previous rune
	for _, r := range source {
		if quoted != 0 {
			result.WriteRune(r)
			if escaped {
				escaped = false
			} else if r == '\\' && quoted != '`' {
				escaped = true
			} else if r == quoted {
				quoted = 0
			}
			previous = r
			continue
		}
		if unicode.IsSpace(r) {
			pending = true
			continue
		}
		if pending && identifier(previous) && (identifier(r) || r == '[' || r == '*' || r == '<') {
			result.WriteByte(' ')
		}
		pending = false
		result.WriteRune(r)
		if r == '"' || r == '`' || r == '\'' {
			quoted = r
		}
		previous = r
	}
	return result.String()
}

// Source restores go/types formatting from a native or canonical named type.
// Commas/semicolons inside quoted tags are data and remain untouched.
func Source(name string) string {
	var result strings.Builder
	quoted, escaped := rune(0), false
	var previous rune
	for _, r := range Compact(name) {
		if quoted != 0 {
			result.WriteRune(r)
			if escaped {
				escaped = false
			} else if r == '\\' && quoted != '`' {
				escaped = true
			} else if r == quoted {
				quoted = 0
			}
			previous = r
			continue
		}
		if r == '"' || r == '`' {
			if identifier(previous) || previous == ']' || previous == '}' {
				result.WriteByte(' ')
			}
			quoted = r
		}
		result.WriteRune(r)
		if r == ',' || r == ';' {
			result.WriteByte(' ')
		}
		previous = r
	}
	return result.String()
}

func identifier(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
