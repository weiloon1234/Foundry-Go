// Package docrefs parses versioned guide references and explicit Markdown anchors.
package docrefs

import (
	"regexp"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var identifier = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Reference names a page and, optionally, an explicit anchor in that page.
type Reference struct {
	File   string
	Line   int
	Guide  string
	Anchor string
}

// Parse reads complete Foundry guides: declarations outside Markdown fences.
// Each line is independent; malformed lines yield no partial references.
func Parse(cache *markdown.Cache, name string, data []byte) ([]Reference, error) {
	var refs []Reference
	var failures []error
	parsed, err := cache.Read(name, data)
	if err != nil {
		return nil, err
	}
	parsed.ProseLines(func(line int, text string) {
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
			failures = append(failures, diagnostic.GuidesDeclaration.At(name, line, 0).
				Say("Foundry guides line", "malformed declaration").
				To("write comma-separated `guide` or `guide#anchor` tokens of lowercase letters, digits and internal hyphens").Err())
			return
		}
		refs = append(refs, parsed...)
	})
	return refs, safeerror.Join("documentation references failed", failures...)
}
