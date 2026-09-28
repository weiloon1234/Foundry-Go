// Command checkdocs checks local documentation links and repository consistency.
// It uses only the standard library so the initial framework needs no dependencies.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	inlineLink    = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	blueprintName = regexp.MustCompile(`^([0-9]{2})-.+\.md$`)
)

func main() {
	if err := checkRepository("."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Documentation links, blueprint numbering, and fixture Go requirements are valid.")
}

func checkRepository(root string) error {
	var failures []error
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
			if err := checkLinks(path); err != nil {
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

func checkLinks(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var failures []error
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	fence := ""
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		for _, match := range inlineLink.FindAllStringSubmatch(maskInlineCode(line), -1) {
			target := strings.Trim(strings.TrimSpace(match[1]), "<>")
			link, err := url.Parse(target)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s:%d: invalid link %q: %w", path, lineNumber, target, err))
				continue
			}
			if link.IsAbs() || link.Host != "" || link.Path == "" {
				continue
			}
			local := filepath.Join(filepath.Dir(path), filepath.FromSlash(link.Path))
			if _, err := os.Stat(local); err != nil {
				failures = append(failures, fmt.Errorf("%s:%d: local link %q: %w", path, lineNumber, target, err))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		failures = append(failures, fmt.Errorf("%s: %w", path, err))
	}
	return errors.Join(failures...)
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
