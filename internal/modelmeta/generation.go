package modelmeta

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// IdentityVariable lowercases the first word, treating an initialism as one word.
func IdentityVariable(table string) string {
	runes := []rune(table)
	end := 1
	for end < len(runes) && unicode.IsUpper(runes[end]) {
		end++
	}
	if end > 1 && end < len(runes) && unicode.IsLower(runes[end]) {
		end--
	}
	for i := 0; i < end && i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes) + "Identity"
}

// FieldTag describes one model field in declaration order. Tag is its whole Go tag.
type FieldTag struct{ Name, Tag string }

// Fingerprint covers ordered field names and the presence and exact value of
// their foundry tag. Length prefixes make the representation unambiguous.
func Fingerprint(fields []FieldTag) string {
	var b strings.Builder
	for _, f := range fields {
		tag, present := reflect.StructTag(f.Tag).Lookup("foundry")
		b.WriteString(strconv.Itoa(len(f.Name)))
		b.WriteByte(':')
		b.WriteString(f.Name)
		b.WriteString(strconv.FormatBool(present))
		b.WriteString(strconv.Itoa(len(tag)))
		b.WriteByte(':')
		b.WriteString(tag)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
