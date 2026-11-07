package docrefs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var anchorLine = regexp.MustCompile(`^<a id="([^"]*)"></a>$`)
var anchorStart = regexp.MustCompile(`^<a\s+[^>]*\bid\b`)

// Anchors indexes only literal explicit IDs, never generated heading slugs.
type Anchors struct{ lines map[string]int }

// IndexAnchors rejects malformed and duplicate explicit anchors outside fences.
// Callers must handle its error before using the index for fragment validation.
func IndexAnchors(name string, data []byte) (Anchors, error) {
	index := Anchors{lines: make(map[string]int)}
	var failures []error
	outsideFences(data, func(line int, text string) {
		text = strings.TrimSpace(text)
		if !anchorStart.MatchString(text) {
			return
		}
		match := anchorLine.FindStringSubmatch(text)
		if match == nil || !identifier.MatchString(match[1]) {
			failures = append(failures, problem(name, line, `invalid explicit anchor; use <a id="lowercase-id"></a> alone on a line`))
			return
		}
		id := match[1]
		if previous, exists := index.lines[id]; exists {
			failures = append(failures, problem(name, line, fmt.Sprintf("duplicate explicit anchor %q (first declared on line %d)", id, previous)))
			return
		}
		index.lines[id] = line
	})
	return index, safeerror.Join("documentation references failed", failures...)
}

// CheckFragment validates a raw fragment without its leading #. Framework
// checkdocs can use this for guide navigation links as well as app references.
func (a Anchors) CheckFragment(file string, line int, fragment string) error {
	if !identifier.MatchString(fragment) {
		return problem(file, line, "invalid explicit anchor fragment "+fmt.Sprintf("%q", fragment))
	}
	if _, exists := a.lines[fragment]; !exists {
		return problem(file, line, fmt.Sprintf("explicit anchor %q is absent", fragment))
	}
	return nil
}

// CheckReferences reads pages only from the selected module's docs/guides.
// It never follows a guide symlink or fetches missing pages from the network.
func CheckReferences(guidesDir string, refs []Reference) error {
	type page struct {
		anchors Anchors
		err     error
	}
	pages := make(map[string]page)
	var failures []error
	for _, ref := range refs {
		if !identifier.MatchString(ref.Guide) {
			failures = append(failures, problem(ref.File, ref.Line, "invalid guide name"))
			continue
		}
		p, loaded := pages[ref.Guide]
		if !loaded {
			name := filepath.Join(guidesDir, ref.Guide+".md")
			info, err := os.Lstat(name)
			if err != nil || !info.Mode().IsRegular() {
				p.err = safeerror.New(fmt.Sprintf("selected framework guide %s is missing or is not a regular file", ref.Guide))
			} else {
				data, err := os.ReadFile(name)
				if err != nil {
					p.err = safeerror.Wrap("cannot read selected framework guide "+ref.Guide, err)
				} else {
					p.anchors, p.err = IndexAnchors(name, data)
				}
			}
			pages[ref.Guide] = p
		}
		if p.err != nil {
			failures = append(failures, safeerror.Prefix(fmt.Sprintf("%s:%d", ref.File, ref.Line), p.err))
		} else if ref.Anchor != "" {
			if err := p.anchors.CheckFragment(ref.File, ref.Line, ref.Anchor); err != nil {
				failures = append(failures, safeerror.Prefix("guide "+ref.Guide, err))
			}
		}
	}
	return safeerror.Join("documentation references failed", failures...)
}
