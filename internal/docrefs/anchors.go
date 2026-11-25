package docrefs

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
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
	markdown.Read(data).AnchorLines(func(line int, text string) {
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
			failures = append(failures, problem(name, line, fmt.Sprintf("duplicate explicit anchor %q (first declared on line %d); rename or remove the second anchor.", id, previous)))
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
		return problem(file, line, "invalid explicit anchor fragment "+fmt.Sprintf("%q; use lowercase letters, digits and internal hyphens.", fragment))
	}
	if _, exists := a.lines[fragment]; !exists {
		return problem(file, line, fmt.Sprintf("explicit anchor %q is absent; link to an existing explicit anchor or add it to the target guide.", fragment))
	}
	return nil
}

// Pages indexes each target at most once per run, including failed reads.
type Pages struct {
	root  string
	pages map[string]page
	bytes int
}
type page struct {
	anchors Anchors
	err     error
}

// NewPages creates a run-local cache confined to root.
func NewPages(root string) *Pages { return &Pages{root: root, pages: make(map[string]page)} }

// Index reuses caller-read bytes while preserving the first snapshot of a target.
func (p *Pages) Index(name string, data []byte) (Anchors, error) {
	name = filepath.Clean(name)
	if cached, ok := p.pages[name]; ok {
		return cached.anchors, cached.err
	}
	var result page
	p.bytes += len(data)
	if len(p.pages) >= markdown.MaxDocuments || p.bytes > markdown.MaxRunBytes {
		result.err = safeerror.New(fmt.Sprintf("documentation:0: anchor index exceeds maximum %d targets or %d bytes; split the documentation run.", markdown.MaxDocuments, markdown.MaxRunBytes))
	} else {
		result.anchors, result.err = IndexAnchors(name, data)
	}
	if len(p.pages) < markdown.MaxDocuments {
		p.pages[name] = result
	}
	return result.anchors, result.err
}

// Load retains both successful indexes and read failures until the run ends.
func (p *Pages) Load(name string) (Anchors, error) {
	name = filepath.Clean(name)
	if cached, ok := p.pages[name]; ok {
		return cached.anchors, cached.err
	}
	if len(p.pages) >= markdown.MaxDocuments {
		return Anchors{}, safeerror.New(fmt.Sprintf("documentation:0: anchor index exceeds maximum %d targets.", markdown.MaxDocuments))
	}
	relative, err := filepath.Rel(p.root, name)
	if err == nil {
		var data []byte
		data, err = markdown.ReadFile(p.root, relative, markdown.MaxDocumentBytes)
		if err == nil {
			return p.Index(name, data)
		}
	}
	p.pages[name] = page{err: err}
	return Anchors{}, err
}

// CheckReferences reads pages only from the selected module's docs/guides.
// It never follows a guide symlink or fetches missing pages from the network.
func CheckReferences(guidesDir string, refs []Reference) error {
	root := guidesDir
	if filepath.Base(guidesDir) == "guides" && filepath.Base(filepath.Dir(guidesDir)) == "docs" {
		root = filepath.Dir(filepath.Dir(guidesDir))
	}
	return NewPages(root).CheckReferences(guidesDir, refs)
}

// CheckReferences shares target indexes with ordinary local links.
func (p *Pages) CheckReferences(guidesDir string, refs []Reference) error {
	var failures []error
	for _, ref := range refs {
		if !identifier.MatchString(ref.Guide) {
			failures = append(failures, problem(ref.File, ref.Line, "invalid guide name; use a guide basename with lowercase letters, digits and internal hyphens."))
			continue
		}
		anchors, err := p.Load(filepath.Join(guidesDir, ref.Guide+".md"))
		if err != nil {
			failures = append(failures, safeerror.Prefix(fmt.Sprintf("%s:%d: selected framework guide %s is missing or is not a regular file; link to a regular guide file in the selected framework version", ref.File, ref.Line, ref.Guide), err))
		} else if ref.Anchor != "" {
			failures = append(failures, anchors.CheckFragment(ref.File, ref.Line, ref.Anchor))
		}
	}
	return safeerror.Join("documentation references failed", failures...)
}
