package docsource

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var directive = regexp.MustCompile(`^<!-- compiled-source: ([^\s#]+)(?:#([a-z0-9]+(?:-[a-z0-9]+)*))? -->$`)
var sectionAnchor = regexp.MustCompile(`^<a id="(agent-entry|recipe-[a-z0-9-]+)"></a>$`)
var containerPrefix = regexp.MustCompile(`^[ ]{0,3}(?:> ?|[-+*] |[0-9]{1,9}[.)] )`)

var taskLink = regexp.MustCompile(`^(?:- )?(?:[^\[\]<>\x60.!?]+: )?\[[^\]]+\]\([^\s)]+\)(?:, \[[^\]]+\]\([^\s)]+\))*$`)

// Inclusion locates a source excerpt and its rendered document block.
type Inclusion struct {
	Document       string
	Line           int
	Source, Region string
	First, Last    int
}

// Document retains authored text and the rendering derived from its sources.
type Document struct {
	// BindingErrors locates executable blocks that need a compiled source marker.
	BindingErrors      []error
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
	paths, err := documentPaths(root)
	if err != nil {
		return nil, err
	}
	var documents []Document
	var failures []error
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
			failures = append(failures, err)
			continue
		}
		documents = append(documents, document)
	}
	return documents, errors.Join(failures...)
}

// Check compares rendered fences with their current source.
func Check(documents []Document) error {
	var failures []error
	for _, doc := range documents {
		if !bytes.Equal(doc.Original, doc.Rendered) {
			location := doc.Path + ":0"
			if len(doc.StaleLines) > 0 {
				location = fmt.Sprintf("%s:%d", doc.Path, doc.StaleLines[0])
			}
			failures = append(failures, safeerror.New(fmt.Sprintf("%s: compiled source differs from the displayed block; run go run ./internal/cmd/renderdocs.", location)))
		}
	}
	return errors.Join(failures...)
}

// CheckBindings reports every executable block without a compiled owner.
func CheckBindings(documents []Document) error {
	var failures []error
	for _, doc := range documents {
		failures = append(failures, doc.BindingErrors...)
	}
	return errors.Join(failures...)
}

func documentPaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%s:0: cannot read teaching directory: %v; restore read access.", filename, walkErr)
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		owned := teachingPath(relative)
		// An owned tree has no name-based exclusions. Never open secret inputs.
		if strings.HasPrefix(entry.Name(), ".env") {
			if owned {
				return fmt.Errorf("%s:0: secret input is inside a teaching directory; move it outside the teaching tree.", relative)
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if owned && entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s:0: symlink inside a teaching directory; replace it with an owned file or directory.", relative)
		}
		if entry.IsDir() {
			if !owned {
				switch entry.Name() {
				case ".git", ".cache", "node_modules", "vendor", ".deploy":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Name() == "AGENTS.md" || owned && strings.HasSuffix(relative, ".md") {
			paths = append(paths, filename)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func teachingPath(name string) bool {
	return name == "docs/guides" || strings.HasPrefix(name, "docs/guides/") || name == ".claude/skills" || strings.HasPrefix(name, ".claude/skills/")
}

// Strip container markers without consuming the indentation of their contents.
func containerLine(line string) string {
	for {
		prefix := containerPrefix.FindString(line)
		if prefix == "" {
			return line
		}
		line = strings.TrimPrefix(line, prefix)
	}
}

// Text and Markdown receive best-effort detection in addition to their tag contract.
var fenceLanguages = map[string]bool{
	"go": true, "typescript": true, "tsx": true,
	"sh": true, "toml": true, "json": true, "sql": true, "text": true,
	"ini": true, "markdown": true, "yaml": true, "makefile": true,
}

var htmlCodeBlock = regexp.MustCompile(`(?i)<(?:pre|code)(?:[\s/>])`)

func fenceProblem(language string) string {
	if !fenceLanguages[language] {
		tags := make([]string, 0, len(fenceLanguages))
		for tag := range fenceLanguages {
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		return "fenced block has no supported language tag; tag the block with one of " + strings.Join(tags, ", ") + "."
	}
	return ""
}

type section struct {
	level int
	agent bool
}

func parseDocument(name string, data []byte, read func(string) (source, error)) (Document, error) {
	result := Document{Path: name, Original: data}
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	var sections []section
	pendingAnchor := ""
	anchorLine := 0
	for i := 0; i < len(lines); i++ {
		normalized := containerLine(lines[i])
		trimmed := strings.TrimSpace(normalized)
		fail := func(message string) (Document, error) {
			return Document{}, safeerror.New(fmt.Sprintf("%s:%d: %s", name, i+1, message))
		}
		if trimmed != "" && indentColumns(normalized) >= 4 {
			result.BindingErrors = append(result.BindingErrors, fmt.Errorf("%s:%d: indented code is unsupported; use a fenced block with a language tag.", name, i+1))
		}
		if htmlCodeBlock.MatchString(MaskInlineCode(normalized)) {
			result.BindingErrors = append(result.BindingErrors, fmt.Errorf("%s:%d: HTML code blocks are unsupported; use a fenced block with a language tag.", name, i+1))
		}
		if strings.HasPrefix(trimmed, "<!-- go-source:") || trimmed == "<!-- compiled-examples -->" || strings.HasPrefix(trimmed, "<!-- orm-example:") {
			return fail("obsolete guide snippet marker; use <!-- compiled-source: path#region -->.")
		}
		if match := sectionAnchor.FindStringSubmatch(trimmed); match != nil {
			if pendingAnchor != "" {
				return fail(fmt.Sprintf("anchor %s must be followed by a # heading; add the heading immediately after the anchor.", pendingAnchor))
			}
			pendingAnchor = match[1]
			anchorLine = i + 1
			out.WriteString(lines[i])
			continue
		}
		if level, title := heading(trimmed); level > 0 {
			for _, parent := range sections {
				if parent.agent && level > parent.level {
					return fail("Agent entry permits task links only, no subsections; move the subsection outside Agent entry.")
				}
			}
			for len(sections) > 0 && sections[len(sections)-1].level >= level {
				sections = sections[:len(sections)-1]
			}
			sections = append(sections, section{level: level, agent: pendingAnchor == "agent-entry" || strings.EqualFold(title, "Agent entry")})
			pendingAnchor = ""
			out.WriteString(lines[i])
			continue
		}
		if trimmed != "" && pendingAnchor != "" {
			return fail(fmt.Sprintf("anchor %s must be followed by a # heading; move intervening content below that heading.", pendingAnchor))
		}
		agent := false
		for _, s := range sections {
			agent = agent || s.agent
		}
		if agent && trimmed != "" && !taskLink.MatchString(trimmed) {
			return fail("Agent entry permits task labels and links only, no fences or prose; move the explanation into a task section.")
		}
		if agent && trimmed != "" {
			result.AgentLines = append(result.AgentLines, i+1)
		}
		if strings.HasPrefix(trimmed, "<!-- compiled-source") {
			match := directive.FindStringSubmatch(trimmed)
			if match == nil {
				return fail("malformed compiled-source directive; use <!-- compiled-source: path#region -->.")
			}
			source, err := read(match[1])
			if err != nil {
				if strings.HasPrefix(err.Error(), match[1]+":") {
					return Document{}, err
				}
				return fail(fmt.Sprintf("compiled source cannot be read: %v; correct its path and region markers.", err))
			}
			selected := source.whole
			if match[2] != "" {
				var found bool
				selected, found = source.regions[match[2]]
				if !found {
					return fail("missing source region " + match[2] + "; bind an existing region or add matching begin and end markers to the source.")
				}
			}
			result.Inclusions = append(result.Inclusions, Inclusion{Document: name, Line: i + 1, Source: match[1], Region: match[2], First: selected.first, Last: selected.last})
			out.WriteString(lines[i])
			j := i + 1
			for j < len(lines) && strings.TrimSpace(containerLine(lines[j])) == "" {
				out.WriteString(lines[j])
				j++
			}
			if j == len(lines) {
				return fail("compiled-source must immediately precede a fence; put a tagged fence directly after the marker.")
			}
			marker, language := FenceStart(containerLine(lines[j]))
			if problem := fenceProblem(language); marker != "" && problem != "" {
				result.BindingErrors = append(result.BindingErrors, fmt.Errorf("%s:%d: %s", name, j+1, problem))
			}
			if language == "tsx" {
				result.BindingErrors = append(result.BindingErrors, fmt.Errorf("%s:%d: TSX block has no compiled source host in the framework; use a compiled plain-TypeScript recipe or move UI teaching to its application.", name, j+1))
			}
			if marker == "" {
				return fail("compiled-source must immediately precede a fence; put a tagged fence directly after the marker.")
			}
			end := fenceEnd(lines, j+1, marker)
			if end < 0 {
				return fail("unclosed compiled-source fence; add a matching closing delimiter.")
			}
			rendered := renderFence(source.language, selected.text, strings.HasSuffix(lines[end], "\n"))
			prefix := lines[j][:len(lines[j])-len(strings.TrimLeft(containerLine(lines[j]), " \t"))]
			if prefix != "" {
				parts := strings.SplitAfter(rendered, "\n")
				for k, part := range parts {
					if part != "" {
						if k == 0 {
							parts[k] = prefix + part
						} else {
							parts[k] = continuationPrefix(prefix) + part
						}
					}
				}
				rendered = strings.Join(parts, "")
			}
			if rendered != strings.Join(lines[j:end+1], "") {
				result.StaleLines = append(result.StaleLines, i+1)
			}
			out.WriteString(rendered)
			i = end
			continue
		}
		nested := normalized
		if marker, language := FenceStart(nested); marker != "" {
			end := fenceEnd(lines, i+1, marker)
			if end < 0 {
				return fail("unclosed fence; add a matching closing delimiter.")
			}
			body := make([]string, 0, end-i-1)
			for _, line := range lines[i+1 : end] {
				body = append(body, containerLine(line))
			}
			code := strings.Join(body, "")
			if problem := fenceProblem(language); problem != "" {
				result.BindingErrors = append(result.BindingErrors, fmt.Errorf("%s:%d: %s", name, i+1, problem))
			}
			if executable(language) || (language == "text" || language == "markdown") && executableTeaching(code) {
				label := "Executable"
				if language == "go" || language == "golang" {
					label = "Go"
				}
				if language == "ts" || language == "typescript" || language == "tsx" {
					label = "TypeScript"
				}
				result.BindingErrors = append(result.BindingErrors, fmt.Errorf("%s:%d: %s block has no compiled source; move the code into a fixture file and bind it with <!-- compiled-source: path#region -->.", name, i+1, label))
			}
			out.WriteString(strings.Join(lines[i:end+1], ""))
			i = end
			continue
		}

		out.WriteString(lines[i])
	}
	if pendingAnchor != "" {
		return Document{}, safeerror.New(fmt.Sprintf("%s:%d: anchor %s must be followed by a # heading before the document ends; add the heading or remove the anchor.", name, anchorLine, pendingAnchor))
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
	language := strings.TrimSpace(line[n:])
	return line[:n], language
}
func fenceEnd(lines []string, start int, marker string) int {
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(containerLine(lines[i]))
		if ClosesFence(line, marker) {
			return i
		}
	}
	return -1
}
func executable(language string) bool {
	switch language {
	case "go", "typescript", "tsx":
		return true
	}
	return false
}

// ClosesFence distinguishes a safe longer delimiter from shorter content runs.
func ClosesFence(line, marker string) bool {
	line = strings.TrimSpace(line)
	return marker != "" && len(line) >= len(marker) && strings.Trim(line, marker[:1]) == ""
}

// Recognize declarations and statements even when a fence hides its language.
var executableStatement = regexp.MustCompile(`(?m)^\s*(?:(?:export\s+)?(?:async\s+)?(?:func|function|interface|type|const|let|var|package|import)\s+[A-Za-z_({*]|[A-Za-z_][\w., ]*\s*:=|(?:return|await)\s+[A-Za-z_]|[A-Za-z_][\w.]*\(|(?:if|for|switch|select|throw|try|catch)\b[^\n]*[({]|(?:go|defer)\s+[A-Za-z_][\w.]*\(|<[A-Z][A-Za-z_0-9]*(?:[ />]))`)

func executableText(code string) bool { return executableStatement.MatchString(code) }

var listPrefix = regexp.MustCompile(`[-+*] |[0-9]{1,9}[.)] `)

// A list opener becomes indentation on subsequent lines; quote markers remain.
func continuationPrefix(prefix string) string {
	return listPrefix.ReplaceAllStringFunc(prefix, func(s string) string { return strings.Repeat(" ", len(s)) })
}

func indentColumns(line string) int {
	columns := 0
	for _, c := range line {
		switch c {
		case ' ':
			columns++
		case '\t':
			columns += 4 - columns%4
		default:
			return columns
		}
	}
	return columns
}

func executableTeaching(code string) bool {
	if executableText(code) {
		return true
	}
	lines := strings.SplitAfter(code, "\n")
	for i, line := range lines {
		marker, language := FenceStart(containerLine(line))
		if marker == "" || !executable(language) {
			continue
		}
		end := fenceEnd(lines, i+1, marker)
		if end < 0 {
			return true
		}
		for _, body := range lines[i+1 : end] {
			if strings.TrimSpace(containerLine(body)) != "" {
				return true
			}
		}
	}
	return false
}
