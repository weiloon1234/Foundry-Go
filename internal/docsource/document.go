package docsource

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var directive = regexp.MustCompile(`^<!-- compiled-source: ([^\s#]+)(?:#([a-z0-9]+(?:-[a-z0-9]+)*))? -->$`)
var sectionAnchor = regexp.MustCompile(`^<a id="(agent-entry|recipe-[a-z0-9-]+)"></a>$`)
var containerPrefix = regexp.MustCompile(`^(?:[ \t]*(?:>[ \t]*|[-+*] |[0-9]+[.)] ))+`)

var taskLink = regexp.MustCompile(`^(?:- )?(?:[^\[\]<>\x60.!?]+: )?\[[^\]]+\]\([^\s)]+\)(?:, \[[^\]]+\]\([^\s)]+\))*$`)

type guidePolicy uint8

const (
	sourceBoundGuide guidePolicy = iota + 1
	migratedQueryGuide
)

// These guides already required every Go fence to name compiled source before
// the region renderer. Keep that coverage while other guides migrate by recipe.
var completeGuides = map[string]guidePolicy{
	"model-references.md": sourceBoundGuide, "model-extensions.md": sourceBoundGuide,
	"model-extension-slots.md": sourceBoundGuide, "attachments.md": sourceBoundGuide,
	"http-model-binding.md": sourceBoundGuide, "scoped-model-binding.md": sourceBoundGuide,
	"model-pagination.md": sourceBoundGuide, "http-pagination.md": sourceBoundGuide,
	"http-cursor-pagination.md": sourceBoundGuide, "model-scaffolding.md": sourceBoundGuide,
	"computed-query-keys.md":           migratedQueryGuide,
	"conditional-expressions.md":       migratedQueryGuide,
	"datatable.md":                     migratedQueryGuide,
	"geographic-distance.md":           migratedQueryGuide,
	"interval-queries.md":              migratedQueryGuide,
	"json-models.md":                   migratedQueryGuide,
	"lateral-joins.md":                 migratedQueryGuide,
	"model-aggregate-filters.md":       migratedQueryGuide,
	"model-aggregates.md":              migratedQueryGuide,
	"model-chunks.md":                  migratedQueryGuide,
	"model-correlations.md":            migratedQueryGuide,
	"model-ctes.md":                    migratedQueryGuide,
	"model-distinct.md":                migratedQueryGuide,
	"model-joins.md":                   migratedQueryGuide,
	"model-projections.md":             migratedQueryGuide,
	"model-queries.md":                 migratedQueryGuide,
	"model-recursive-ctes.md":          migratedQueryGuide,
	"model-set-operations.md":          migratedQueryGuide,
	"model-subqueries.md":              migratedQueryGuide,
	"model-windows.md":                 migratedQueryGuide,
	"scalar-calculations.md":           migratedQueryGuide,
	"temporal-calculations.md":         migratedQueryGuide,
	"transaction-correlations.md":      migratedQueryGuide,
	"transaction-query-composition.md": migratedQueryGuide,
	"trigram-search.md":                migratedQueryGuide,
	"typed-field-computations.md":      migratedQueryGuide,
	"value-comparisons-and-joins.md":   migratedQueryGuide,
	"window-ranges-and-names.md":       migratedQueryGuide,
}

// RequiresORMMigration identifies guides whose complete coverage and migration
// policy came from the former compiled-examples convention.
func RequiresORMMigration(name string) bool {
	return completeGuides[filepath.Base(name)] == migratedQueryGuide
}

type Inclusion struct {
	Document       string
	Line           int
	Source, Region string
	First, Last    int
}

type Document struct {
	Path               string
	Original, Rendered []byte
	Inclusions         []Inclusion
	StaleLines         []int
	// AgentLines identifies navigation lines using the same section boundaries
	// as recipe coverage. Link targets are checked by checkdocs through docrefs.
	AgentLines []int
}

// Load reads all guide directives. It validates ownership and section coverage,
// but leaves rendering comparison and publication to the caller.
func Load(root string) ([]Document, error) {
	paths, err := filepath.Glob(filepath.Join(root, "docs", "guides", "*.md"))
	if err != nil {
		return nil, err
	}
	var documents []Document
	var inclusions []Inclusion
	sources := make(map[string]source)
	for _, filename := range paths {
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return nil, err
		}
		document, err := parseDocument(filepath.ToSlash(relative), data, func(name string) (source, error) {
			if cached, ok := sources[name]; ok {
				return cached, nil
			}
			value, err := readSource(root, name)
			if err == nil {
				sources[name] = value
			}
			return value, err
		})
		if err != nil {
			return nil, err
		}
		inclusions = append(inclusions, document.Inclusions...)
		documents = append(documents, document)
	}
	if err := uniqueOwners(inclusions); err != nil {
		return nil, err
	}
	return documents, nil
}

func Check(documents []Document) error {
	var failures []error
	for _, doc := range documents {
		if !bytes.Equal(doc.Original, doc.Rendered) {
			location := doc.Path
			if len(doc.StaleLines) > 0 {
				location = fmt.Sprintf("%s:%d", doc.Path, doc.StaleLines[0])
			}
			failures = append(failures, safeerror.New(fmt.Sprintf("%s: compiled-source fence differs; run make generate", location)))
		}
	}
	return safeerror.Join("documentation check failed", failures...)
}

func uniqueOwners(inclusions []Inclusion) error {
	for i, current := range inclusions {
		for _, prior := range inclusions[:i] {
			if prior.Source == current.Source && (prior.Region == current.Region || prior.Region == "" || current.Region == "" || prior.First <= current.Last && current.First <= prior.Last) {
				return safeerror.New(fmt.Sprintf("%s:%d: duplicate or overlapping inclusion of %s#%s; owner is %s:%d", current.Document, current.Line, current.Source, current.Region, prior.Document, prior.Line))
			}
		}
	}
	return nil
}

type section struct {
	level         int
	recipe, agent bool
}

func parseDocument(name string, data []byte, read func(string) (source, error)) (Document, error) {
	result := Document{Path: name, Original: data}
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	var sections []section
	pendingAnchor := ""
	anchorLine := 0
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		fail := func(message string) (Document, error) {
			return Document{}, safeerror.New(fmt.Sprintf("%s:%d: %s", name, i+1, message))
		}
		if strings.HasPrefix(trimmed, "<!-- go-source:") || trimmed == "<!-- compiled-examples -->" {
			return fail("obsolete guide snippet marker; use an explicit compiled-source region")
		}
		if match := sectionAnchor.FindStringSubmatch(trimmed); match != nil {
			if pendingAnchor != "" {
				return fail(fmt.Sprintf("anchor %s on line %d must be followed by one ATX heading", pendingAnchor, anchorLine))
			}
			pendingAnchor = match[1]
			anchorLine = i + 1
			out.WriteString(lines[i])
			continue
		}
		if level, title := heading(trimmed); level > 0 {
			for _, parent := range sections {
				if parent.agent && level > parent.level {
					return fail("Agent entry permits task links only, no subsections")
				}
			}
			for len(sections) > 0 && sections[len(sections)-1].level >= level {
				sections = sections[:len(sections)-1]
			}
			sections = append(sections, section{level: level, recipe: strings.HasPrefix(pendingAnchor, "recipe-"), agent: pendingAnchor == "agent-entry" || strings.EqualFold(title, "Agent entry")})
			pendingAnchor = ""
			out.WriteString(lines[i])
			continue
		}
		if trimmed != "" && pendingAnchor != "" {
			return fail(fmt.Sprintf("anchor %s on line %d must be followed by one ATX heading; intervening content and setext headings are not supported", pendingAnchor, anchorLine))
		}
		recipe, agent := false, false
		for _, s := range sections {
			recipe = recipe || s.recipe
			agent = agent || s.agent
		}
		if agent && trimmed != "" && !taskLink.MatchString(trimmed) {
			return fail("Agent entry permits task labels and links only, no fences or prose")
		}
		if agent && trimmed != "" {
			result.AgentLines = append(result.AgentLines, i+1)
		}
		if strings.HasPrefix(trimmed, "<!-- compiled-source") {
			match := directive.FindStringSubmatch(trimmed)
			if match == nil {
				return fail("malformed compiled-source directive")
			}
			source, err := read(match[1])
			if err != nil {
				return Document{}, safeerror.Prefix(fmt.Sprintf("%s:%d", name, i+1), err)
			}
			selected := source.whole
			if match[2] != "" {
				var found bool
				selected, found = source.regions[match[2]]
				if !found {
					return fail("missing source region " + match[2])
				}
			}
			result.Inclusions = append(result.Inclusions, Inclusion{Document: name, Line: i + 1, Source: match[1], Region: match[2], First: selected.first, Last: selected.last})
			out.WriteString(lines[i])
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				out.WriteString(lines[j])
				j++
			}
			if j == len(lines) {
				return fail("compiled-source must immediately precede a fence")
			}
			marker, _ := FenceStart(lines[j])
			if marker == "" {
				return fail("compiled-source must immediately precede a fence")
			}
			end := fenceEnd(lines, j+1, marker)
			if end < 0 {
				return fail("unclosed compiled-source fence")
			}
			rendered := renderFence(source.language, selected.text, strings.HasSuffix(lines[end], "\n"))
			if rendered != strings.Join(lines[j:end+1], "") {
				result.StaleLines = append(result.StaleLines, i+1)
			}
			out.WriteString(rendered)
			i = end
			continue
		}
		if recipe || completeGuides[filepath.Base(name)] != 0 {
			nested := containerPrefix.ReplaceAllString(lines[i], "")
			if nested != lines[i] {
				if marker, language := FenceStart(nested); marker != "" && executable(language) {
					return fail("executable recipe fences must be source-bound top-level blocks")
				}
			}
		}
		if marker, language := FenceStart(lines[i]); marker != "" {
			if (recipe || completeGuides[filepath.Base(name)] != 0) && executable(language) {
				return fail("executable recipe fence requires compiled-source")
			}
			end := fenceEnd(lines, i+1, marker)
			if end < 0 {
				return fail("unclosed fence")
			}
			out.WriteString(strings.Join(lines[i:end+1], ""))
			i = end
			continue
		}
		out.WriteString(lines[i])
	}
	if pendingAnchor != "" {
		return Document{}, safeerror.New(fmt.Sprintf("%s:%d: anchor %s must be followed by one ATX heading before EOF", name, anchorLine, pendingAnchor))
	}
	result.Rendered = []byte(out.String())
	return result, nil
}

func heading(line string) (int, string) {
	n := len(line) - len(strings.TrimLeft(line, "#"))
	if n < 1 || n > 6 || n == len(line) || line[n] != ' ' {
		return 0, ""
	}
	return n, strings.TrimSpace(strings.TrimRight(line[n+1:], "#"))
}

// FenceStart returns a fenced block delimiter and its language.
func FenceStart(line string) (string, string) {
	line = strings.TrimSpace(line)
	if len(line) < 3 || line[0] != '`' && line[0] != '~' {
		return "", ""
	}
	n := 0
	for n < len(line) && line[n] == line[0] {
		n++
	}
	if n < 3 {
		return "", ""
	}
	info := strings.Fields(line[n:])
	language := ""
	if len(info) > 0 {
		language = strings.ToLower(info[0])
	}
	return line[:n], language
}
func fenceEnd(lines []string, start int, marker string) int {
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if ClosesFence(line, marker) {
			return i
		}
	}
	return -1
}
func executable(language string) bool {
	switch language {
	case "go", "golang", "ts", "typescript", "tsx":
		return true
	}
	return false
}

// ClosesFence distinguishes a safe longer delimiter from shorter content runs.
func ClosesFence(line, marker string) bool {
	line = strings.TrimSpace(line)
	return marker != "" && len(line) >= len(marker) && strings.Trim(line, marker[:1]) == ""
}
