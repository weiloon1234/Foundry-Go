package docrefs

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var anchorLine = regexp.MustCompile(`^<a id="([^"]*)"></a>$`)
var anchorStart = regexp.MustCompile(`^<a\s+[^>]*\bid\b`)

// Anchors indexes literal explicit IDs for reference checks, never generated heading
// slugs. It also records the slugs of the document's headings, which only CheckLink
// accepts, because a Markdown viewer resolves them.
type Anchors struct {
	lines map[string]int
	slugs map[string]bool
}

// IndexAnchors rejects malformed and duplicate explicit anchors outside fences.
// Callers must handle its error before using the index for fragment validation.
func IndexAnchors(cache *markdown.Cache, name string, data []byte) (Anchors, error) {
	index := Anchors{lines: make(map[string]int), slugs: make(map[string]bool)}
	var failures []error
	parsed, err := cache.Read(name, data)
	if err != nil {
		return Anchors{}, err
	}
	repeats := make(map[string]int)
	for _, block := range parsed.Blocks {
		if block.Kind != markdown.Heading {
			continue
		}
		slug := headingSlug(block.Text)
		index.slugs[slug] = true
		if repeats[slug]++; repeats[slug] > 1 {
			index.slugs[fmt.Sprintf("%s-%d", slug, repeats[slug]-1)] = true
		}
	}
	parsed.AnchorLines(func(line int, text string) {
		text = strings.TrimSpace(text)
		if !anchorStart.MatchString(text) {
			return
		}
		match := anchorLine.FindStringSubmatch(text)
		if match == nil || !identifier.MatchString(match[1]) {
			failures = append(failures, diagnostic.AnchorInvalid.At(name, line, 0).
				Say(fmt.Sprintf("explicit anchor %q", clip(text)), "malformed").
				To(`write <a id="lowercase-id"></a> alone on a line`).Err())
			return
		}
		id := match[1]
		if previous, exists := index.lines[id]; exists {
			failures = append(failures, diagnostic.AnchorDuplicate.At(name, line, 0).
				Say(fmt.Sprintf("anchor %q", id), fmt.Sprintf("declared twice (first on line %d)", previous)).
				To("rename or remove the second anchor").Err())
			return
		}
		index.lines[id] = line
	})
	return index, safeerror.Join("documentation references failed", failures...)
}

// CheckFragment validates a raw fragment without its leading #. Framework
// checkdocs can use this for guide navigation links as well as app references.
// page names the page the fragment points into, as the message should read it.
func (a Anchors) CheckFragment(file string, line int, page, fragment string) error {
	if !identifier.MatchString(fragment) {
		return diagnostic.AnchorInvalid.At(file, line, 0).
			Say(fmt.Sprintf("fragment %q", fragment), "not an anchor id").
			To("use lowercase letters, digits and internal hyphens").Err()
	}
	if _, exists := a.lines[fragment]; !exists {
		return diagnostic.AnchorMissing.At(file, line, 0).
			Say(fmt.Sprintf("anchor %q of %s", fragment, page), "not declared").
			To("link to an existing explicit anchor or add it there").Err()
	}
	return nil
}

// CheckLink validates the fragment of a link between documents: an explicit anchor,
// or the slug a Markdown viewer gives a heading. A fragment that names neither is a
// dead link.
func (a Anchors) CheckLink(file string, line int, page, fragment string) error {
	if _, exists := a.lines[fragment]; exists || a.slugs[fragment] {
		return nil
	}
	return diagnostic.AnchorMissing.At(file, line, 0).
		Say(fmt.Sprintf("anchor %q of %s", clip(fragment), page), "not declared").
		To("link to an existing explicit anchor or heading, or add the anchor there").Err()
}

// headingSlug is the fragment a Markdown viewer gives a heading: lowercase, spaces
// as hyphens, and only letters, digits, hyphens and underscores kept.
func headingSlug(title string) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r == ' ':
			slug.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			slug.WriteRune(r)
		}
	}
	return slug.String()
}

// clip bounds quoted source text in a message.
func clip(text string) string {
	const limit = 64
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit], "") + "..."
}

// Pages indexes each target at most once per run, including failed reads.
type Pages struct {
	root  string
	cache *markdown.Cache
	pages map[string]page
	bytes int
}
type page struct {
	anchors Anchors
	err     error
}

// NewPages creates a run-local cache confined to root. Documents are parsed
// through cache, shared with the run's other checks; nil parses privately.
func NewPages(root string, cache *markdown.Cache) *Pages {
	return &Pages{root: root, cache: cache, pages: make(map[string]page)}
}

// documentName is the repository-relative slash path the parser and its size
// rule know a page by.
func (p *Pages) documentName(name string) string {
	if relative, err := filepath.Rel(p.root, name); err == nil {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(name)
}

// Index reuses caller-read bytes while preserving the first snapshot of a target.
func (p *Pages) Index(name string, data []byte) (Anchors, error) {
	name = filepath.Clean(name)
	if cached, ok := p.pages[name]; ok {
		return cached.anchors, cached.err
	}
	var result page
	p.bytes += len(data)
	if len(p.pages) >= markdown.MaxDocuments || p.bytes > markdown.MaxRunBytes {
		result.err = diagnostic.DocLimit.At("documentation", 0, 0).
			Say("anchor index", fmt.Sprintf("over %d targets or %d bytes", markdown.MaxDocuments, markdown.MaxRunBytes)).
			To("split the documentation run").Err()
	} else {
		result.anchors, result.err = IndexAnchors(p.cache, p.documentName(name), data)
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
		return Anchors{}, diagnostic.DocLimit.At("documentation", 0, 0).
			Say("anchor index", fmt.Sprintf("over %d targets", markdown.MaxDocuments)).
			To("split the documentation run").Err()
	}
	relative, err := filepath.Rel(p.root, name)
	if err == nil {
		var data []byte
		data, err = markdown.ReadFile(p.root, relative, markdown.DocumentLimit(relative))
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
	return NewPages(root, nil).CheckReferences(guidesDir, refs)
}

// CheckReferences shares target indexes with ordinary local links.
func (p *Pages) CheckReferences(guidesDir string, refs []Reference) error {
	var failures []error
	for _, ref := range refs {
		if !identifier.MatchString(ref.Guide) {
			failures = append(failures, diagnostic.GuideName.At(ref.File, ref.Line, 0).
				Say(fmt.Sprintf("guide %q", ref.Guide), "not a guide name").
				To("use a guide basename of lowercase letters, digits and internal hyphens").Err())
			continue
		}
		anchors, err := p.Load(filepath.Join(guidesDir, ref.Guide+".md"))
		if err != nil {
			failures = append(failures, diagnostic.GuideMissing.At(ref.File, ref.Line, 0).
				Say("guide "+ref.Guide, "missing or not a regular file in the selected framework").
				To("check the name against the guides of the pinned framework, or remove the reference").Err())
		} else if ref.Anchor != "" {
			failures = append(failures, anchors.CheckFragment(ref.File, ref.Line, "guide "+ref.Guide, ref.Anchor))
		}
	}
	return safeerror.Join("documentation references failed", failures...)
}
