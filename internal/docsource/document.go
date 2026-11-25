package docsource

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/yuin/goldmark/ast"
)

var directive = regexp.MustCompile(`^<!-- compiled-source: ([^\s#]+)(?:#([a-z0-9]+(?:-[a-z0-9]+)*))? -->$`)
var sectionAnchor = regexp.MustCompile(`^<a id="(agent-entry|recipe-[a-z0-9-]+)"></a>$`)

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
	total := 0
	for _, filename := range paths {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return nil, err
		}
		data, err := markdown.ReadFile(root, relative, markdown.MaxDocumentBytes)
		if err != nil {
			return nil, err
		}
		total += len(data)
		if total > markdown.MaxRunBytes {
			return nil, safeerror.New(fmt.Sprintf("documentation:0: inputs exceed maximum %d bytes per run; split the teaching corpus.", markdown.MaxRunBytes))
		}
		document, err := parseDocument(filepath.ToSlash(relative), data, func(name string) (source, error) {
			if cached, ok := sources[name]; ok {
				return cached, nil
			}
			value, err := readSource(root, name)
			if err == nil {
				total += value.bytes
				if total > markdown.MaxRunBytes {
					return source{}, safeerror.New(fmt.Sprintf("documentation:0: inputs exceed maximum %d bytes per run.", markdown.MaxRunBytes))
				}
				sources[name] = value
			}
			return value, err
		})
		if err != nil {
			failures = append(failures, err)
			continue
		}
		total += len(document.Rendered)
		if total > markdown.MaxRunBytes {
			return nil, safeerror.New(fmt.Sprintf("documentation:0: retained inputs and renderings exceed maximum %d bytes per run.", markdown.MaxRunBytes))
		}
		documents = append(documents, document)
	}
	return documents, safeerror.Join("documentation check failed", failures...)
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
	return safeerror.Join("documentation check failed", failures...)
}

// CheckBindings reports every executable block without a compiled owner.
func CheckBindings(documents []Document) error {
	var failures []error
	for _, doc := range documents {
		failures = append(failures, doc.BindingErrors...)
	}
	return safeerror.Join("documentation check failed", failures...)
}

func documentPaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return safeerror.Owned(filename+":0: cannot read teaching directory; restore read access.", walkErr)
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
				return safeerror.New(fmt.Sprintf("%s:0: secret input is inside a teaching directory; move it outside the teaching tree.", relative))
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if (owned || relative == "docs" || relative == ".claude" || entry.Name() == "AGENTS.md" || relative == ".") && entry.Type()&os.ModeSymlink != 0 {
			return safeerror.New(fmt.Sprintf("%s:0: symlink inside a teaching directory; replace it with an owned file or directory.", relative))
		}
		if entry.IsDir() {
			if entry.Name() == "AGENTS.md" || owned && strings.HasSuffix(entry.Name(), ".md") {
				return safeerror.New(relative + ":0: instruction must be a regular file.")
			}
			if !owned {
				switch entry.Name() {
				case ".git", ".cache", "node_modules", "vendor", ".deploy":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Name() == "AGENTS.md" || owned && strings.HasSuffix(relative, ".md") {
			if !entry.Type().IsRegular() {
				return safeerror.New(relative + ":0: teaching document must be a regular file.")
			}
			paths = append(paths, filename)
			if len(paths) > markdown.MaxDocuments {
				return safeerror.New(fmt.Sprintf("documentation:0: too many documents; maximum is %d.", markdown.MaxDocuments))
			}
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func teachingPath(name string) bool {
	return name == "docs/guides" || strings.HasPrefix(name, "docs/guides/") || name == ".claude/skills" || strings.HasPrefix(name, ".claude/skills/")
}

// Text and Markdown receive best-effort detection in addition to their tag contract.
var fenceLanguages = map[string]bool{
	"go": true, "typescript": true, "tsx": true,
	"sh": true, "toml": true, "json": true, "sql": true, "text": true,
	"ini": true, "markdown": true, "yaml": true, "makefile": true,
}

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
	parsed := markdown.Read(data)
	anchors := make(map[int]string)
	parsed.AnchorLines(func(line int, text string) {
		if match := sectionAnchor.FindStringSubmatch(strings.TrimSpace(text)); match != nil {
			anchors[line] = match[1]
		}
	})
	byNode := make(map[ast.Node]markdown.Node)
	for _, block := range parsed.Blocks {
		byNode[block.AST] = block
	}
	var out bytes.Buffer
	cursor := 0
	var sections []section
	pendingAnchor := ""
	anchorLine := 0
	for _, block := range parsed.Blocks {
		trimmed := strings.TrimSpace(block.Text)
		fail := func(message string) (Document, error) {
			return Document{}, safeerror.New(fmt.Sprintf("%s:%d: %s", name, block.First, message))
		}
		binding := func(message string) {
			result.BindingErrors = append(result.BindingErrors, safeerror.New(fmt.Sprintf("%s:%d: %s", name, block.First, message)))
		}
		if block.Kind == markdown.Comment {
			if strings.HasPrefix(trimmed, "<!-- go-source:") || trimmed == "<!-- compiled-examples -->" || strings.HasPrefix(trimmed, "<!-- orm-example:") {
				return fail("obsolete guide snippet marker; use <!-- compiled-source: path#region -->.")
			}
		}
		anchor := ""
		for line := block.First; line <= block.Last; line++ {
			if id := anchors[line]; id != "" {
				anchor = id
				if block.Last != line {
					return fail("anchor " + id + " must be followed by a # heading; move intervening content below that heading.")
				}
			}
		}
		if anchor != "" {
			if pendingAnchor != "" {
				return fail("anchor " + pendingAnchor + " must be followed by a # heading; add the heading immediately after the anchor.")
			}
			for _, parent := range sections {
				if parent.agent {
					lines := strings.Split(strings.TrimSuffix(block.Text, "\n"), "\n")
					for i, line := range lines[:len(lines)-1] {
						if !taskLink.MatchString(strings.TrimSpace(line)) {
							return fail("Agent entry permits task labels and links only, no fences or prose; move the explanation into a task section.")
						}
						result.AgentLines = append(result.AgentLines, block.First+i)
					}
				}
			}
			pendingAnchor, anchorLine = anchor, block.Last
			continue
		}

		if block.Kind == markdown.Heading {
			if pendingAnchor != "" && !block.ATX {
				return fail("anchor " + pendingAnchor + " must be followed by a # heading; use an ATX heading.")
			}
			for _, parent := range sections {
				if parent.agent && block.Level > parent.level {
					return fail("Agent entry permits task links only, no subsections; move the subsection outside Agent entry.")
				}
			}
			for len(sections) > 0 && sections[len(sections)-1].level >= block.Level {
				sections = sections[:len(sections)-1]
			}
			sections = append(sections, section{level: block.Level, agent: pendingAnchor == "agent-entry" || strings.EqualFold(trimmed, "Agent entry")})
			pendingAnchor = ""
			continue
		}
		if pendingAnchor != "" && trimmed != "" {
			return fail("anchor " + pendingAnchor + " must be followed by a # heading; move intervening content below that heading.")
		}
		agent := false
		for _, section := range sections {
			agent = agent || section.agent
		}
		if agent && trimmed != "" {
			if block.Kind != markdown.Prose {
				return fail("Agent entry permits task labels and links only, no fences or prose; move the explanation into a task section.")
			}
			for i, line := range strings.Split(strings.TrimSuffix(block.Text, "\n"), "\n") {
				if !taskLink.MatchString(strings.TrimSpace(line)) {
					return fail("Agent entry permits task labels and links only, no fences or prose; move the explanation into a task section.")
				}
				result.AgentLines = append(result.AgentLines, block.First+i)
			}
		}
		if block.Kind == markdown.Indented {
			binding("indented code is unsupported; use a fenced block with a language tag.")
		}
		if block.Kind == markdown.HTML && markdown.HasCodeElement(block.Text) {
			binding("HTML code blocks are unsupported; use a fenced block with a language tag.")
		}
		if block.Kind == markdown.Comment && strings.HasPrefix(trimmed, "<!-- compiled-source") {
			if directive.FindStringSubmatch(trimmed) == nil {
				return fail("malformed compiled-source directive; use <!-- compiled-source: path#region -->.")
			}
			if next, ok := byNode[block.AST.NextSibling()]; !ok || next.Kind != markdown.Fence {
				return fail("compiled-source must immediately precede a fence in the same container; put a tagged fence directly after the marker.")
			}
		}
		if block.Kind != markdown.Fence {
			continue
		}
		if problem := fenceProblem(block.Info); problem != "" {
			binding(problem)
		}
		previous := byNode[block.AST.PreviousSibling()]
		match := directive.FindStringSubmatch(strings.TrimSpace(previous.Text))
		if previous.Kind != markdown.Comment {
			match = nil
		}
		if match == nil {
			if executable(block.Info) || (block.Info == "text" || block.Info == "markdown") && executableTeaching(block.Text) {
				label := "Executable"
				if block.Info == "go" {
					label = "Go"
				}
				if block.Info == "typescript" || block.Info == "tsx" {
					label = "TypeScript"
				}
				binding(label + " block has no compiled source; move the code into a fixture file and bind it with <!-- compiled-source: path#region -->.")
			}
			continue
		}
		if !block.Closed {
			return fail("unclosed compiled-source fence; add a matching closing delimiter.")
		}
		if block.Info == "tsx" {
			binding("TSX block has no compiled source host in the framework; use a compiled plain-TypeScript recipe or move UI teaching to its application.")
		}
		if read == nil {
			return fail("compiled source cannot be read; provide an owned source reader.")
		}
		source, err := read(match[1])
		if err != nil {
			if message, owned := safeerror.OwnedMessage(err); owned && strings.HasPrefix(message, match[1]+":") {
				return Document{}, err
			}
			return Document{}, safeerror.Prefix(fmt.Sprintf("%s:%d", name, previous.First), err)
		}
		selected := source.whole
		if match[2] != "" {
			var found bool
			selected, found = source.regions[match[2]]
			if !found {
				return fail("missing source region " + match[2] + "; bind an existing region or add matching begin and end markers to the source.")
			}
		}
		if source.language != block.Info {
			return fail("compiled source language differs from the fence tag; use the source language tag.")
		}
		result.Inclusions = append(result.Inclusions, Inclusion{Document: name, Line: previous.First, Source: match[1], Region: match[2], First: selected.first, Last: selected.last})
		var body strings.Builder
		for _, line := range strings.SplitAfter(selected.text, "\n") {
			if line != "" {
				if body.Len()+len(block.Prefix)+len(line)+1 > markdown.MaxDocumentBytes {
					return fail(fmt.Sprintf("rendered document exceeds maximum %d bytes; split the source excerpt.", markdown.MaxDocumentBytes))
				}
				body.WriteString(block.Prefix)
				body.WriteString(line)
				if !strings.HasSuffix(line, "\n") {
					body.WriteByte('\n')
				}
			}
		}
		rendered := body.String()
		if out.Len()+len(data)-cursor-(block.BodyEnd-block.BodyStart)+len(rendered) > markdown.MaxDocumentBytes {
			return fail(fmt.Sprintf("rendered document exceeds maximum %d bytes; split the source excerpt.", markdown.MaxDocumentBytes))
		}
		// Reparse the replacement using the same parser: a source delimiter must not
		// close the author's fence. Delimiters themselves are never rewritten.
		opener := data[block.Start:block.BodyStart]
		closing := data[block.BodyEnd+len(block.Prefix) : block.End]
		plain := selected.text
		if plain != "" && !strings.HasSuffix(plain, "\n") {
			plain += "\n"
		}
		candidate := append([]byte(nil), opener...)
		candidate = append(candidate, plain...)
		candidate = append(candidate, closing...)
		replacement := markdown.Read(candidate).Blocks
		valid := len(replacement) == 1 && replacement[0].Kind == markdown.Fence && replacement[0].Closed && replacement[0].BodyEnd == len(opener)+len(plain)
		if !valid {
			return fail("compiled source closes the fence; choose a longer fence delimiter.")
		}
		if !bytes.Equal(data[block.BodyStart:block.BodyEnd], []byte(rendered)) {
			result.StaleLines = append(result.StaleLines, previous.First)
		}
		out.Write(data[cursor:block.BodyStart])
		out.WriteString(rendered)
		cursor = block.BodyEnd
	}
	for _, inline := range parsed.Inlines {
		if inline.Kind == markdown.HTML && markdown.HasCodeElement(inline.Text) {
			result.BindingErrors = append(result.BindingErrors, safeerror.New(fmt.Sprintf("%s:%d: HTML code blocks are unsupported; use a fenced block with a language tag.", name, inline.First)))
		}
	}
	if pendingAnchor != "" {
		return Document{}, safeerror.New(fmt.Sprintf("%s:%d: anchor %s must be followed by a # heading before the document ends; add the heading or remove the anchor.", name, anchorLine, pendingAnchor))
	}
	out.Write(data[cursor:])
	result.Rendered = out.Bytes()
	return result, nil
}

func executable(language string) bool {
	return language == "go" || language == "typescript" || language == "tsx"
}

// Recognize declarations and statements even when a fence hides its language.
var executableStatement = regexp.MustCompile(`(?m)^\s*(?:(?:export\s+)?(?:async\s+)?(?:func|function|interface|type|const|let|var|package|import)\s+[A-Za-z_({*]|[A-Za-z_][\w., ]*\s*:=|(?:return|await)\s+[A-Za-z_]|[A-Za-z_][\w.]*\(|(?:if|for|switch|select|throw|try|catch)\b[^\n]*[({]|(?:go|defer)\s+[A-Za-z_][\w.]*\(|<[A-Z][A-Za-z_0-9]*(?:[ />]))`)

func executableTeaching(code string) bool {
	if executableStatement.MatchString(code) {
		return true
	}
	for _, block := range markdown.Read([]byte(code)).Blocks {
		if block.Kind == markdown.Fence && executable(block.Info) && strings.TrimSpace(block.Text) != "" {
			return true
		}
	}
	return false
}
