// Package httppath shares HTTP pattern grammar between runtime and generation.
package httppath

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"unicode/utf8"
)

// Segment is a literal or named parameter in the shared route grammar.
type Segment struct {
	Literal string
	Name    string
	Tail    bool
}

// Parse validates route syntax without binding a concrete DTO or starting I/O.
func Parse(pattern string) ([]Segment, error) {
	if pattern == "" || pattern[0] != '/' || !utf8.ValidString(pattern) {
		return nil, fault.New(fault.Invalid, "route path must start with a slash")
	}
	parts := strings.Split(pattern[1:], "/")
	segments := make([]Segment, 0, len(parts))
	names := make(map[string]bool)
	for i, part := range parts {
		if part == "" && i != len(parts)-1 {
			return nil, fault.New(fault.Invalid, "route path contains an empty segment")
		}
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := part[1 : len(part)-1]
			tail := strings.HasSuffix(name, "...")
			if tail {
				name = strings.TrimSuffix(name, "...")
			}
			if !validPathName(name) || names[name] || tail && i != len(parts)-1 {
				return nil, fault.New(fault.Invalid, "route path has an invalid or duplicate parameter")
			}
			names[name] = true
			segments = append(segments, Segment{Name: name, Tail: tail})
			continue
		}
		if part == "." || part == ".." || strings.ContainsAny(part, "{}%?#\\") || !ValidText(part) {
			return nil, fault.New(fault.Invalid, "route path contains an invalid literal segment")
		}
		segments = append(segments, Segment{Literal: part})
	}
	return segments, nil
}

func validPathName(name string) bool {
	for i, ch := range name {
		if ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || i > 0 && ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return name != ""
}

// ValidText checks lossless UTF-8 and excludes control characters.
func ValidText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, ch := range text {
		if ch < ' ' || ch == 127 {
			return false
		}
	}
	return true
}
