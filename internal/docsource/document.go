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

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var directive = regexp.MustCompile(`^<!-- compiled-source: ([^\s#]+)(?:#([a-z0-9]+(?:-[a-z0-9]+)*))? -->$`)

// A worked-example region is delimited the way an application's baseline
// delimits any app-owned region: the two marker lines stand alone.
var appRegionMarker = regexp.MustCompile(`^<!-- app-owned:(begin|end) ([a-z0-9-]+) -->$`)

const workedExample = "worked-example"

// The explicit statement that an application has no code for a task: exactly
// this sentence and one Foundry guides line naming one recipe anchor.
const noWorkedExample = "This application has no worked example for this task yet."

var noWorkedExampleGuide = regexp.MustCompile("^Foundry guides: `[a-z0-9]+(?:-[a-z0-9]+)*#recipe-[a-z0-9]+(?:-[a-z0-9]+)*`$")

var sectionAnchor = regexp.MustCompile(`^<a id="(agent-entry|recipe-[a-z0-9-]+)"></a>$`)

var taskLink = regexp.MustCompile(`^(?:- )?(?:[^\[\]<>\x60.!?]+: )?\[[^\]]+\]\([^\s)]+\)(?:, \[[^\]]+\]\([^\s)]+\))*$`)

// finding starts a finding of rule at a document line; an application's findings
// teach the rule's application reference.
func finding(policy Policy, rule diagnostic.Rule, path string, line int) diagnostic.Diagnostic {
	found := rule.At(path, line, 0)
	if policy.words().application {
		found = found.InApplication()
	}
	return found
}

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
	policy     Policy
}

// Load reads all guide directives of the framework repository. It validates
// ownership and section coverage, but leaves rendering comparison and
// publication to the caller.
func Load(root string, cache *markdown.Cache) ([]Document, error) {
	paths, err := documentPaths(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(paths))
	for i, filename := range paths {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return nil, err
		}
		names[i] = filepath.ToSlash(relative)
	}
	return LoadDocuments(FrameworkPolicy(), root, names, cache)
}

// LoadDocuments reads the named documents (slash-separated, relative to root)
// and binds their code blocks to sources the policy approves. The caller owns
// document discovery.
func LoadDocuments(policy Policy, root string, names []string, cache *markdown.Cache) ([]Document, error) {
	if len(names) > markdown.MaxDocuments {
		return nil, diagnostic.DocLimit.At("documentation", 0, 0).Say("run", fmt.Sprintf("more than %d documents", markdown.MaxDocuments)).To("split the documentation run").Err()
	}
	var documents []Document
	var failures []error
	sources := make(map[string]source)
	total := 0
	for _, relative := range names {
		data, err := markdown.ReadFile(root, relative, markdown.DocumentLimit(relative))
		if err != nil {
			return nil, err
		}
		total += len(data)
		if total > markdown.MaxRunBytes {
			return nil, diagnostic.DocLimit.At("documentation", 0, 0).Say("inputs", fmt.Sprintf("over %d bytes per run", markdown.MaxRunBytes)).To("split the teaching corpus").Err()
		}
		document, err := parseDocument(policy, cache, relative, data, func(name string) (source, error) {
			// Approval depends on the document, so it is never cached.
			language, err := policy.approve(root, relative, name)
			if err != nil {
				return source{}, err
			}
			if cached, ok := sources[name]; ok {
				return cached, nil
			}
			value, err := loadSource(policy, root, name, language)
			if _, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); errors.Is(statErr, fs.ErrNotExist) && policy.words().rebind != "" {
				// A source the application removed or renamed: say how to re-point the binding.
				return source{}, safeerror.New("compiled source cannot be read; restore the source file" + policy.words().rebind)
			}
			if err == nil {
				total += value.bytes
				if total > markdown.MaxRunBytes {
					return source{}, diagnostic.DocLimit.At("documentation", 0, 0).Say("inputs", fmt.Sprintf("over %d bytes per run", markdown.MaxRunBytes)).To("split the teaching corpus").Err()
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
			return nil, diagnostic.DocLimit.At("documentation", 0, 0).Say("retained inputs and renderings", fmt.Sprintf("over %d bytes per run", markdown.MaxRunBytes)).To("split the teaching corpus").Err()
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
			line := 0
			if len(doc.StaleLines) > 0 {
				line = doc.StaleLines[0]
			}
			failures = append(failures, finding(doc.policyOrFramework(), diagnostic.DocRendered, doc.Path, line).Text("", "compiled source differs from the displayed block; "+doc.policyOrFramework().words().renderFix).Err())
		}
	}
	return safeerror.Join("documentation check failed", failures...)
}

// Render rewrites each document whose bound blocks differ from their sources,
// byte for byte outside the replaced block bodies, and writes nothing else. It
// opens every target for writing before it writes any, so a target that cannot be
// written leaves every document unchanged. A failure while writing names the
// documents already written and the one that failed.
func Render(root string, documents []Document) error {
	type target struct {
		path string
		data []byte
		file *os.File
	}
	var targets []target
	defer func() {
		for _, t := range targets {
			if t.file != nil {
				t.file.Close()
			}
		}
	}()
	for _, document := range documents {
		if bytes.Equal(document.Original, document.Rendered) {
			continue
		}
		file, err := markdown.OpenRegular(root, document.Path, os.O_WRONLY)
		if err != nil {
			return err
		}
		targets = append(targets, target{document.Path, document.Rendered, file})
	}
	var written []string
	for i := range targets {
		t := &targets[i]
		err := t.file.Truncate(0)
		if err == nil {
			_, err = t.file.Write(t.data)
		}
		closeErr := t.file.Close()
		t.file = nil
		if err == nil {
			err = closeErr
		}
		if err != nil {
			already := "no other document was written"
			if len(written) > 0 {
				already = "already written: " + strings.Join(written, ", ")
			}
			return diagnostic.DocFile.At(t.path, 0, 0).Say("document", "could not be written").To("make it writable and run render again; " + already).Caused(err)
		}
		written = append(written, t.path)
	}
	return nil
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
			return diagnostic.DocFile.At(filename, 0, 0).Say("teaching directory", "cannot be read").To("restore read access").Caused(walkErr)
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		owned := teachingPath(relative)
		// An owned tree has no name-based exclusions. Never open secret inputs.
		if strings.HasPrefix(strings.ToLower(entry.Name()), ".env") {
			if owned {
				return diagnostic.DocFile.At(relative, 0, 0).Say("secret input", "found inside a teaching directory").To("move it outside the teaching tree").Err()
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if (owned || relative == "docs" || relative == ".claude" || entry.Name() == "AGENTS.md" || relative == ".") && entry.Type()&os.ModeSymlink != 0 {
			return diagnostic.DocFile.At(relative, 0, 0).Say("symlink", "found inside a teaching directory").To("replace it with an owned file or directory").Err()
		}
		if entry.IsDir() {
			if entry.Name() == "AGENTS.md" || owned && strings.HasSuffix(entry.Name(), ".md") {
				return diagnostic.DocFile.At(relative, 0, 0).Say("instruction", "not a regular file").To("use a regular file").Err()
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
				return diagnostic.DocFile.At(relative, 0, 0).Say("teaching document", "not a regular file").To("use a regular file").Err()
			}
			paths = append(paths, filename)
			if len(paths) > markdown.MaxDocuments {
				return diagnostic.DocLimit.At("documentation", 0, 0).Say("run", fmt.Sprintf("more than %d documents", markdown.MaxDocuments)).To("split the documentation run").Err()
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

func (d Document) policyOrFramework() Policy {
	if d.policy == nil {
		return FrameworkPolicy()
	}
	return d.policy
}

func parseDocument(policy Policy, cache *markdown.Cache, name string, data []byte, read func(string) (source, error)) (Document, error) {
	result := Document{Path: name, Original: data, policy: policy}
	words := policy.words()
	parsed, err := cache.Read(name, data)
	if err != nil {
		return Document{}, err
	}
	anchors := make(map[int]string)
	parsed.AnchorLines(func(line int, text string) {
		if match := sectionAnchor.FindStringSubmatch(strings.TrimSpace(text)); match != nil {
			anchors[line] = match[1]
		}
	})
	// sibling resolves an adjacent block of the same container; a node that is
	// not a block (a list, an absent neighbour) yields the zero Node.
	sibling := func(index int) markdown.Node {
		if index < 0 {
			return markdown.Node{}
		}
		return parsed.Blocks[index]
	}
	var out bytes.Buffer
	cursor := 0
	example := exampleRegion{}
	var sections []section
	pendingAnchor := ""
	anchorLine := 0
	for _, block := range parsed.Blocks {
		trimmed := strings.TrimSpace(block.Text)
		fail := func(rule diagnostic.Rule, message string) (Document, error) {
			return Document{}, finding(policy, rule, name, block.First).Text("", message).Err()
		}
		binding := func(message string) {
			result.BindingErrors = append(result.BindingErrors, finding(policy, diagnostic.DocBinding, name, block.First).Text("", message).Err())
		}
		if block.Kind == markdown.Comment {
			if strings.HasPrefix(trimmed, "<!-- go-source:") || trimmed == "<!-- compiled-examples -->" || strings.HasPrefix(trimmed, "<!-- orm-example:") {
				return fail(diagnostic.DocMarker, "obsolete guide snippet marker; use <!-- compiled-source: path#region -->.")
			}
		}
		if policy.requiresWorkedExample() && example.observe(&result, name, block) {
			continue
		}
		anchor := ""
		for line := block.First; line <= block.Last; line++ {
			if id := anchors[line]; id != "" {
				anchor = id
				if block.Last != line {
					return fail(diagnostic.DocSectionAnchor, "anchor "+id+" must be followed by a # heading; move intervening content below that heading.")
				}
			}
		}
		if anchor != "" {
			if pendingAnchor != "" {
				return fail(diagnostic.DocSectionAnchor, "anchor "+pendingAnchor+" must be followed by a # heading; add the heading immediately after the anchor.")
			}
			for _, parent := range sections {
				if parent.agent {
					lines := strings.Split(strings.TrimSuffix(block.Text, "\n"), "\n")
					for i, line := range lines[:len(lines)-1] {
						if !taskLink.MatchString(strings.TrimSpace(line)) {
							return fail(diagnostic.DocAgentEntry, "Agent entry permits task labels and links only, no fences or prose; move the explanation into a task section.")
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
				return fail(diagnostic.DocSectionAnchor, "anchor "+pendingAnchor+" must be followed by a # heading; use an ATX heading.")
			}
			for _, parent := range sections {
				if parent.agent && block.Level > parent.level {
					return fail(diagnostic.DocAgentEntry, "Agent entry permits task links only, no subsections; move the subsection outside Agent entry.")
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
			return fail(diagnostic.DocSectionAnchor, "anchor "+pendingAnchor+" must be followed by a # heading; move intervening content below that heading.")
		}
		agent := false
		for _, section := range sections {
			agent = agent || section.agent
		}
		if agent && trimmed != "" {
			if block.Kind != markdown.Prose {
				return fail(diagnostic.DocAgentEntry, "Agent entry permits task labels and links only, no fences or prose; move the explanation into a task section.")
			}
			for i, line := range strings.Split(strings.TrimSuffix(block.Text, "\n"), "\n") {
				if !taskLink.MatchString(strings.TrimSpace(line)) {
					return fail(diagnostic.DocAgentEntry, "Agent entry permits task labels and links only, no fences or prose; move the explanation into a task section.")
				}
				result.AgentLines = append(result.AgentLines, block.First+i)
			}
		}
		if block.Kind == markdown.Indented {
			binding("indented code is unsupported; use a fenced block with a language tag.")
		}
		if block.Kind == markdown.HTML && markdown.HasCodeElement(block.Text) {
			binding("HTML pre/code elements are unsupported; use a fenced block with a language tag.")
		}
		if block.Kind == markdown.HTML && strings.HasPrefix(trimmed, "<!-- compiled-source") {
			return fail(diagnostic.DocMarker, "compiled-source marker must stand alone on its line; remove the text after \"-->\".")
		}
		if block.Kind == markdown.Comment && strings.HasPrefix(trimmed, "<!-- compiled-source") {
			if directive.FindStringSubmatch(trimmed) == nil {
				return fail(diagnostic.DocMarker, "malformed compiled-source directive; use <!-- compiled-source: path#region -->.")
			}
			if next := sibling(block.Next); block.Next < 0 || next.Kind != markdown.Fence {
				return fail(diagnostic.DocMarker, "compiled-source must immediately precede a fence in the same container; put a tagged fence directly after the marker.")
			}
		}
		if block.Kind != markdown.Fence {
			continue
		}
		if problem := fenceProblem(block.Info); problem != "" {
			binding(problem)
		}
		previous := sibling(block.Previous)
		match := directive.FindStringSubmatch(strings.TrimSpace(previous.Text))
		if previous.Kind != markdown.Comment {
			match = nil
		}
		if match == nil {
			if executable(block.Info) || (block.Info == "text" || block.Info == "markdown") && executableTeaching(name, block.Text) {
				label := "Executable"
				if block.Info == "go" {
					label = "Go"
				}
				if block.Info == "typescript" || block.Info == "tsx" {
					label = "TypeScript"
				}
				message := label + " block has no compiled source; " + words.bindHint
				if !executable(block.Info) {
					message += " If it is not code, retag it sh or json, or write it as prose."
				}
				binding(message)
			}
			continue
		}
		if !block.Closed {
			return fail(diagnostic.DocMarker, "unclosed compiled-source fence; add a matching closing delimiter.")
		}
		if block.Info == "tsx" && !policy.hostsTSX() {
			binding("TSX block has no compiled source host in the framework; use a compiled plain-TypeScript recipe or move UI teaching to its application.")
		}
		if read == nil {
			return fail(diagnostic.DocMarker, "compiled source cannot be read; provide an owned source reader.")
		}
		source, err := read(match[1])
		if err != nil {
			if message, owned := safeerror.OwnedMessage(err); owned && strings.HasPrefix(message, match[1]+":") {
				return Document{}, err
			} else if owned {
				return fail(diagnostic.DocMarker, message)
			}
			return fail(diagnostic.DocMarker, "compiled source cannot be read; restore the source file"+words.rebind)
		}
		selected := source.whole
		if match[2] != "" {
			var found bool
			selected, found = source.regions[match[2]]
			if !found {
				return fail(diagnostic.DocRegion, "missing source region "+match[2]+"; bind an existing region or add matching begin and end markers to the source"+words.sentenceEnd())
			}
		}
		if source.language != block.Info {
			return fail(diagnostic.DocMarker, "compiled source language differs from the fence tag; use the source language tag.")
		}
		example.bound(selected.text)
		result.Inclusions = append(result.Inclusions, Inclusion{Document: name, Line: previous.First, Source: match[1], Region: match[2], First: selected.first, Last: selected.last})
		// Body lines keep the document's own line ending and the opener's
		// container prefix; an empty line carries no trailing whitespace.
		limit := int(markdown.DocumentLimit(name))
		newline := "\n"
		if block.BodyStart >= 2 && string(data[block.BodyStart-2:block.BodyStart]) == "\r\n" {
			newline = "\r\n"
		}
		var body strings.Builder
		for _, line := range strings.SplitAfter(selected.text, "\n") {
			if line != "" {
				if body.Len()+len(block.Prefix)+len(line)+len(newline) > limit {
					return fail(diagnostic.DocLimit, fmt.Sprintf("rendered document exceeds maximum %d bytes; split the source excerpt.", limit))
				}
				line = strings.TrimSuffix(line, "\n")
				if line == "" {
					body.WriteString(strings.TrimRight(block.Prefix, " \t"))
				} else {
					body.WriteString(block.Prefix)
					body.WriteString(line)
				}
				body.WriteString(newline)
			}
		}
		rendered := body.String()
		if out.Len()+len(data)-cursor-(block.BodyEnd-block.BodyStart)+len(rendered) > limit {
			return fail(diagnostic.DocLimit, fmt.Sprintf("rendered document exceeds maximum %d bytes; split the source excerpt.", limit))
		}
		// Reparse the replacement using the same parser: a source delimiter must not
		// close the author's fence. Delimiters themselves are never rewritten.
		opener := data[block.Start:block.BodyStart]
		closing := data[block.BodyEnd+len(block.ClosePrefix) : block.End]
		plain := selected.text
		if plain != "" && !strings.HasSuffix(plain, "\n") {
			plain += "\n"
		}
		candidate := append([]byte(nil), opener...)
		candidate = append(candidate, plain...)
		candidate = append(candidate, closing...)
		reread, err := markdown.Read(name, candidate)
		if err != nil {
			return fail(diagnostic.DocLimit, "compiled source breaks the document shape limits (line length, quote or list nesting, line endings); correct the source excerpt.")
		}
		replacement := reread.Blocks
		valid := len(replacement) == 1 && replacement[0].Kind == markdown.Fence && replacement[0].Closed && replacement[0].BodyEnd == len(opener)+len(plain)
		if !valid {
			return fail(diagnostic.DocMarker, "compiled source closes the fence; choose a longer fence delimiter.")
		}
		if !bytes.Equal(data[block.BodyStart:block.BodyEnd], []byte(rendered)) {
			result.StaleLines = append(result.StaleLines, previous.First)
		}
		out.Write(data[cursor:block.BodyStart])
		out.WriteString(rendered)
		cursor = block.BodyEnd
	}
	reported := make(map[int]bool)
	for _, inline := range parsed.Inlines {
		if inline.Kind == markdown.HTML && markdown.HasCodeElement(inline.Text) && !reported[inline.First] {
			reported[inline.First] = true
			result.BindingErrors = append(result.BindingErrors, finding(policy, diagnostic.DocBinding, name, inline.First).Text("", "inline <pre> or <code> HTML in prose is unsupported; use backticks for a code span or a fenced block with a language tag").Err())
		}
	}
	example.finish(&result, name)
	if pendingAnchor != "" {
		return Document{}, diagnostic.DocSectionAnchor.At(name, anchorLine, 0).Text("", fmt.Sprintf("anchor %s must be followed by a # heading before the document ends; add the heading or remove the anchor", pendingAnchor)).Err()
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

func executableTeaching(name, code string) bool {
	if executableStatement.MatchString(code) {
		return true
	}
	// A fence body is part of a document already shaped by Read; a refusal here
	// would repeat that diagnostic, so an unreadable fragment is not executable.
	fragment, err := markdown.Read(name, []byte(code))
	if err != nil {
		return false
	}
	for _, block := range fragment.Blocks {
		if block.Kind == markdown.Fence && executable(block.Info) && strings.TrimSpace(block.Text) != "" {
			return true
		}
	}
	return false
}

// exampleRegion follows the worked-example region of one document. A region
// holds a bound block, or exactly the statement that the application has no
// worked example yet together with a Foundry guides line naming a recipe anchor.
type exampleRegion struct {
	open, seen bool
	begin      int
	// fences counts every fence of the region, blocks those that are bound, and
	// code the bound blocks that hold a line of code.
	fences, blocks, code int
	lines                []string
}

// bound records a bound block; text is the source region it displays.
func (r *exampleRegion) bound(text string) {
	if r.open {
		r.blocks++
		if hasCode(text) {
			r.code++
		}
	}
}

// hasCode reports whether text has a line that is neither blank nor only a comment
// (Go, TypeScript and TSX share // and /* */ comments).
func hasCode(text string) bool {
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for line != "" {
			if inBlock {
				end := strings.Index(line, "*/")
				if end < 0 {
					line = ""
					break
				}
				inBlock = false
				line = strings.TrimSpace(line[end+2:])
				continue
			}
			if strings.HasPrefix(line, "//") {
				line = ""
			} else if strings.HasPrefix(line, "/*") {
				inBlock = true
				line = line[2:]
			} else {
				return true
			}
		}
	}
	return false
}

func (r *exampleRegion) fail(result *Document, name string, line int, message string) {
	result.BindingErrors = append(result.BindingErrors, diagnostic.DocWorkedExample.At(name, line, 0).Text("", message).Err())
}

// observe consumes the region markers and collects what lies between them;
// it reports true for a block that is a worked-example marker.
func (r *exampleRegion) observe(result *Document, name string, block markdown.Node) bool {
	trimmed := strings.TrimSpace(block.Text)
	if block.Kind == markdown.Comment {
		if match := appRegionMarker.FindStringSubmatch(trimmed); match != nil && match[2] == workedExample {
			switch {
			case match[1] == "begin" && (r.open || r.seen):
				r.fail(result, name, block.First, "duplicate "+workedExample+" region; keep one region per skill.")
			case match[1] == "begin":
				*r = exampleRegion{open: true, seen: true, begin: block.First}
			case !r.open:
				r.fail(result, name, block.First, workedExample+" region ends without a matching begin marker; add the begin marker above the region.")
			default:
				r.check(result, name, block.First)
				r.open = false
			}
			return true
		}
	}
	if r.open && block.Kind == markdown.Fence {
		r.fences++
	}
	if !r.open || block.Kind == markdown.Fence || block.Kind == markdown.Comment && strings.HasPrefix(trimmed, "<!-- compiled-source") {
		return false
	}
	for _, line := range strings.Split(block.Text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			r.lines = append(r.lines, line)
		}
	}
	return false
}

func (r *exampleRegion) check(result *Document, name string, end int) {
	statement := false
	for _, line := range r.lines {
		statement = statement || line == noWorkedExample
	}
	switch {
	case r.blocks > 0 && statement:
		r.fail(result, name, r.begin, workedExample+" region holds a bound block and also says there is no worked example; remove the statement.")
	case r.code > 0:
	case r.blocks > 0:
		r.fail(result, name, r.begin, workedExample+" region binds a block that holds no code, only blank lines or comments; bind a region with a declaration or statement from the application's own code.")
	case r.fences > 0:
		r.fail(result, name, r.begin, workedExample+" region holds a fence that is not bound; bind it with <!-- compiled-source: path#region -->, or remove it so the region holds exactly the statement \""+noWorkedExample+"\" and one Foundry guides line.")
	case len(r.lines) == 0:
		r.fail(result, name, r.begin, workedExample+" region is empty; bind a block from the application's own code with <!-- compiled-source: path#region -->, or write \""+noWorkedExample+"\" followed by a Foundry guides line naming one recipe anchor.")
	case len(r.lines) == 2 && r.lines[0] == noWorkedExample && noWorkedExampleGuide.MatchString(r.lines[1]):
	default:
		r.fail(result, name, r.begin, workedExample+" region neither binds a block nor makes the explicit statement; hold a bound block, or exactly the sentence \""+noWorkedExample+"\" and one line of the form Foundry guides: `guide#recipe-anchor`, nothing else.")
	}
}

func (r *exampleRegion) finish(result *Document, name string) {
	if r.open {
		r.fail(result, name, r.begin, workedExample+" region is never closed; add the end marker after the region.")
	}
}
