// Command checkdocs checks local documentation links and repository consistency.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
	"github.com/weiloon1234/Foundry-Go/internal/docsource"
	"github.com/weiloon1234/Foundry-Go/internal/retirement"
)

var (
	inlineLink    = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	blueprintName = regexp.MustCompile(`^([0-9]{2})-.+\.md$`)
)

func main() {
	inputs := flag.String("go-inputs", "", "check guide coverage against fixture go list JSON")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "checkdocs accepts flags only")
		os.Exit(1)
	}
	if err := run(".", *inputs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Documentation links, current guide milestone claims, blueprint numbering, and fixture Go requirements are valid.")
}

func run(root, inputs string) error {
	if inputs == "" {
		return checkRepository(root)
	}
	documents, err := docsource.Load(root)
	if err != nil {
		return err
	}
	file, err := os.Open(inputs)
	if err != nil {
		return err
	}
	defer file.Close()
	return docsource.CheckGoInputs(root, documents, file)
}

func checkRepository(root string) error {
	var failures []error
	var references []docrefs.Reference
	agentLines := make(map[string]map[int]bool)
	documents, sourceErr := docsource.Load(root)
	if sourceErr != nil {
		failures = append(failures, sourceErr)
	} else {
		failures = append(failures, docsource.Check(documents), docsource.CheckTypeScriptInputs(root, documents))
		for _, document := range documents {
			path := filepath.Join(root, filepath.FromSlash(document.Path))
			_, err := docrefs.IndexAnchors(path, document.Original)
			failures = append(failures, err)
			lines := make(map[int]bool, len(document.AgentLines))
			for _, line := range document.AgentLines {
				lines[line] = true
			}
			agentLines[path] = lines
		}
	}
	completed, err := completedMilestones(root)
	if err != nil {
		failures = append(failures, err)
	}
	if err := checkBlueprints(root); err != nil {
		failures = append(failures, err)
	}
	version, err := goRequirement(filepath.Join(root, "go.mod"))
	if err != nil {
		failures = append(failures, err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".cache", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			active := currentGuide(root, path) || entry.Name() == "AGENTS.md" || entry.Name() == "README.md" && !strings.HasPrefix(relative, "blueprint/") && !strings.HasPrefix(relative, "docs/evidence/") && !strings.Contains(relative, "/testdata/")
			skill := strings.HasPrefix(relative, ".claude/skills/") && !datedGuide.MatchString(relative)
			if active || skill {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				parsed, err := docrefs.Parse(relative, data)
				references = append(references, parsed...)
				failures = append(failures, err)
				if active {
					if err := retirement.CheckDocument(relative, data); err != nil {
						failures = append(failures, err)
					}
				}
			}
			if err := checkDocument(root, path, currentGuide(root, path), completed, agentLines[path]); err != nil {
				failures = append(failures, err)
			}
		}
		if entry.Name() == "go.mod" && path != filepath.Join(root, "go.mod") && version != "" {
			fixtureVersion, err := goRequirement(path)
			if err != nil {
				failures = append(failures, err)
			} else if fixtureVersion != version {
				failures = append(failures, fmt.Errorf("%s: Go requirement %s differs from root %s", path, fixtureVersion, version))
			}
		}
		return nil
	})
	if err != nil {
		failures = append(failures, err)
	}
	failures = append(failures, docrefs.CheckReferences(filepath.Join(root, "docs", "guides"), references))
	return errors.Join(failures...)
}

func checkBlueprints(root string) error {
	entries, err := os.ReadDir(filepath.Join(root, "blueprint"))
	if err != nil {
		return err
	}
	next := 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "README.md" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		match := blueprintName.FindStringSubmatch(entry.Name())
		if match == nil {
			return fmt.Errorf("blueprint/%s: expected NN-descriptive-name.md", entry.Name())
		}
		number, _ := strconv.Atoi(match[1])
		if number != next {
			return fmt.Errorf("blueprint/%s: expected milestone %02d (missing or duplicate number)", entry.Name(), next)
		}
		next++
	}
	if next == 0 {
		return errors.New("blueprint: no numbered blueprints found")
	}
	return nil
}

func goRequirement(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("%s: missing Go requirement", path)
}

func checkDocument(root, path string, current bool, completed map[string]bool, agentLines map[int]bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	definitions := linkDefinitions(data)
	var failures []error
	if current {
		failures = append(failures, checkMigrationGuide(path, data))
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	fence := ""
	lineNumber := 0
	var paragraph []string
	paragraphLine := 0
	flushParagraph := func() {
		if current && len(paragraph) > 0 {
			for _, match := range pendingMilestone.FindAllStringSubmatch(strings.Join(paragraph, " "), -1) {
				number := match[1]
				if number == "" {
					number = match[2]
				}
				if len(number) == 1 {
					number = "0" + number
				}
				if completed[number] {
					failures = append(failures, fmt.Errorf("%s:%d: future-work claim names completed milestone %s; describe its current API or move dated evidence to a dated record", path, paragraphLine, number))
				}
			}
		}
		paragraph = nil
	}
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if docsource.ClosesFence(line, fence) {
				fence = ""
			}
			continue
		}
		if marker, _ := docsource.FenceStart(line); marker != "" {
			flushParagraph()
			fence = marker
			continue
		}
		masked := maskInlineCode(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			flushParagraph()
		} else if current {
			if len(paragraph) == 0 {
				paragraphLine = lineNumber
			}
			paragraph = append(paragraph, strings.Join(strings.Fields(masked), " "))
		}
		for _, destination := range linkDestinations(masked, definitions) {
			target := strings.Trim(strings.TrimSpace(destination), "<>")
			link, err := url.Parse(target)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s:%d: invalid link %q: %w", path, lineNumber, target, err))
				continue
			}
			explicit := link.Fragment == "agent-entry" || strings.HasPrefix(link.Fragment, "recipe-")
			if agentLines[lineNumber] && (!explicit || link.IsAbs() || link.Host != "") {
				failures = append(failures, fmt.Errorf("%s:%d: Agent entry link %q must target a local guide's explicit agent-entry or recipe-* anchor", path, lineNumber, target))
				continue
			}
			if link.IsAbs() || link.Host != "" || link.Path == "" && !explicit {
				continue
			}
			local := filepath.Join(filepath.Dir(path), filepath.FromSlash(link.Path))
			if link.Path == "" {
				local = path
			}
			relative, err := filepath.Rel(root, local)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				failures = append(failures, fmt.Errorf("%s:%d: local link %q: target is outside repository root", path, lineNumber, target))
				continue
			}
			if _, err := os.Stat(local); err != nil {
				failures = append(failures, fmt.Errorf("%s:%d: local link %q: %w", path, lineNumber, target, err))
				continue
			}
			resolvedRoot, rootErr := filepath.EvalSymlinks(root)
			resolvedLocal, localErr := filepath.EvalSymlinks(local)
			if rootErr != nil || localErr != nil {
				failures = append(failures, fmt.Errorf("%s:%d: local link %q: %w", path, lineNumber, target, errors.Join(rootErr, localErr)))
				continue
			}
			relative, err = filepath.Rel(resolvedRoot, resolvedLocal)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				failures = append(failures, fmt.Errorf("%s:%d: local link %q: target is outside repository root", path, lineNumber, target))
				continue
			}
			if agentLines[lineNumber] && (filepath.Dir(local) != filepath.Join(root, "docs", "guides") || filepath.Ext(local) != ".md") {
				failures = append(failures, fmt.Errorf("%s:%d: Agent entry link %q must target a local guide", path, lineNumber, target))
				continue
			}
			if explicit {
				if err := checkExplicitFragment(path, lineNumber, local, link.Fragment); err != nil {
					failures = append(failures, err)
				}
			}
		}
	}
	flushParagraph()
	if err := scanner.Err(); err != nil {
		failures = append(failures, fmt.Errorf("%s: %w", path, err))
	}
	return errors.Join(failures...)
}

func checkExplicitFragment(source string, line int, target, fragment string) error {
	data, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("%s:%d: fragment target %s: %w", source, line, target, err)
	}
	anchors, err := docrefs.IndexAnchors(target, data)
	if err != nil {
		return err
	}
	if err := anchors.CheckFragment(source, line, fragment); err != nil {
		return fmt.Errorf("%w (target %s)", err, target)
	}
	return nil
}

// Inline Go generics (for example `New[T](arg)`) resemble Markdown links.
// Mask matched code spans, including spans delimited by multiple backticks,
// without hiding real links elsewhere on the same line. Unmatched delimiters
// remain ordinary text. This checker is deliberately not a full Markdown parser.
func maskInlineCode(line string) string {
	masked := []byte(line)
	for start := 0; start < len(line); {
		if line[start] != '`' {
			start++
			continue
		}
		end := start
		for end < len(line) && line[end] == '`' {
			end++
		}
		width := end - start
		closed := -1
		for cursor := end; cursor < len(line); {
			if line[cursor] != '`' {
				cursor++
				continue
			}
			next := cursor
			for next < len(line) && line[next] == '`' {
				next++
			}
			if next-cursor == width {
				closed = next
				break
			}
			cursor = next
		}
		if closed < 0 {
			start = end
			continue
		}
		for i := start; i < closed; i++ {
			masked[i] = ' '
		}
		start = closed
	}
	return string(masked)
}
