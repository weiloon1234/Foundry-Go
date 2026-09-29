// Package filename owns display-name normalization shared by HTTP file transport.
// A display filename is never a filesystem path or a storage object key.
package filename

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 255

// Normalize removes path components, controls, invisible format/bidi marks
// and surrounding quotes. Long names retain a short safe extension and remain
// bounded valid UTF-8. Results own their bytes instead of retaining a larger
// incoming MIME header.
func Normalize(name, fallback string) string {
	name = strings.ToValidUTF8(name, "")
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) != 0 {
		name = parts[len(parts)-1]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || invisible(r) || r == '/' || r == '\\' {
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

// StripInvisible removes zero-width and bidirectional formatting characters
// (U+200B–U+200F, U+202A–U+202E, U+2060–U+2064, U+2066–U+2069, U+061C and
// U+FEFF). They can hide or visually reorder a displayed name, for example
// making "invoice\u202Efdp.exe" render as "invoiceexe.pdf".
func StripInvisible(name string) string {
	return strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, name)
}
func invisible(r rune) bool {
	return r >= 0x200B && r <= 0x200F || r >= 0x202A && r <= 0x202E || r >= 0x2060 && r <= 0x2064 || r >= 0x2066 && r <= 0x2069 || r == 0x061C || r == 0xFEFF
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
