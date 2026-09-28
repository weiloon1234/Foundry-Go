// Package filename owns display-name normalization shared by HTTP file transport.
// A display filename is never a filesystem path or a storage object key.
package filename

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 255

// Normalize removes path components, controls and surrounding quotes. Long
// names retain a short safe extension and remain bounded valid UTF-8.
// Results own their bytes instead of retaining a larger incoming MIME header.
func Normalize(name, fallback string) string {
	name = strings.ToValidUTF8(name, "")
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) != 0 {
		name = parts[len(parts)-1]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = trimDisplayName(name)
	if name == "" || name == "." || name == ".." {
		name = fallback
	}
	if len(name) <= MaxBytes {
		return strings.Clone(name)
	}
	extension := Extension(name)
	if extension == "" {
		return strings.Clone(trimDisplayName(Truncate(name, MaxBytes)))
	}
	// Preserve the complete basename, including earlier dots.
	base := name[:strings.LastIndexByte(name, '.')]
	return Truncate(base, MaxBytes-len(extension)-1) + "." + extension
}

func Extension(name string) string {
	at := strings.LastIndexByte(name, '.')
	if at < 0 {
		return ""
	}
	ext := name[at+1:]
	if len(ext) == 0 || len(ext) > 32 {
		return ""
	}
	for _, r := range ext {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return ""
		}
	}
	return strings.ToLower(ext)
}

func Truncate(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

func trimDisplayName(name string) string {
	return strings.TrimFunc(name, func(r rune) bool { return unicode.IsSpace(r) || r == '\'' || r == '"' })
}
