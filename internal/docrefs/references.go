// Package docrefs parses versioned guide references and explicit Markdown anchors.
package docrefs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var identifier = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Problem locates invalid documentation without requiring a source checkout.
type Problem struct {
	File    string
	Line    int
	Message string
}

func (p Problem) Error() string { return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Message) }

// problem captures the concrete location diagnostic before exposing its value.
func problem(file string, line int, message string) error {
	p := Problem{File: file, Line: line, Message: message}
	return safeerror.Owned(p.Error(), p)
}

// Reference names a page and, optionally, an explicit anchor in that page.
type Reference struct {
	File   string
	Line   int
	Guide  string
	Anchor string
}

// Parse reads complete Foundry guides: declarations outside Markdown fences.
// Each line is independent; malformed lines yield no partial references.
func Parse(name string, data []byte) ([]Reference, error) {
	var refs []Reference
	var failures []error
	outsideFences(data, func(line int, text string) {
		const prefix = "Foundry guides:"
		text = strings.TrimSpace(text)
		if !strings.HasPrefix(text, prefix) {
			return
		}
		var parsed []Reference
		valid := true
		for _, part := range strings.Split(strings.TrimPrefix(text, prefix), ",") {
			part = strings.TrimSpace(part)
			if len(part) < 3 || part[0] != '`' || part[len(part)-1] != '`' {
				valid = false
				break
			}
			guide, anchor, fragment := strings.Cut(part[1:len(part)-1], "#")
			if !identifier.MatchString(guide) || fragment && !identifier.MatchString(anchor) {
				valid = false
				break
			}
			parsed = append(parsed, Reference{File: name, Line: line, Guide: guide, Anchor: anchor})
		}
		if !valid {
			failures = append(failures, problem(name, line, "invalid Foundry guides: declaration; use comma-separated `guide` or `guide#anchor` tokens with lowercase letters, digits and internal hyphens"))
			return
		}
		refs = append(refs, parsed...)
	})
	return refs, safeerror.Join("documentation references failed", failures...)
}

// outsideFences handles both Markdown fence characters, including longer outer
// fences containing shorter examples. A closing fence cannot have trailing text.
func outsideFences(data []byte, visit func(int, string)) {
	var fence byte
	width := 0
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		n := 0
		if len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
		}
		if width > 0 {
			if n >= width && trimmed[0] == fence && strings.TrimSpace(trimmed[n:]) == "" {
				width = 0
			}
			continue
		}
		if n >= 3 && (trimmed[0] != '`' || !strings.Contains(trimmed[n:], "`")) {
			fence, width = trimmed[0], n
			continue
		}
		visit(i+1, line)
	}
}
