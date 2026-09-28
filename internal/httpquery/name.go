package httpquery

import "strings"

// ValidName is the shared declaration grammar for runtime and generation.
// Brackets and dots are literal query-name characters, not nesting instructions.
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("_.-[]", ch) {
			continue
		}
		return false
	}
	return true
}
