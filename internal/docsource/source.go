// Package docsource renders guide examples from compiled consumer source.
package docsource

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var trailingMarker = regexp.MustCompile(`^(.*\S)[ \t]+(// doc-example:(?:begin|end) [a-z0-9]+(?:-[a-z0-9]+)*)$`)

var regionMarker = regexp.MustCompile(`^// doc-example:(begin|end) ([a-z0-9]+(?:-[a-z0-9]+)*)$`)

type excerpt struct {
	text        string
	first, last int
}

type source struct {
	language string
	whole    excerpt
	regions  map[string]excerpt
}

func readSource(root, name string) (source, error) {
	if name == "" || path.Clean(name) != name || strings.ContainsAny(name, `\:#`) || strings.HasPrefix(name, "/") {
		return source{}, fmt.Errorf("invalid repository-relative source path %q", name)
	}
	ext := path.Ext(name)
	approved := (strings.HasPrefix(name, "tests/fixtures/consumer/") || strings.HasPrefix(name, "tests/fixtures/goway/")) && ext == ".go"
	if ext == ".ts" {
		approved = (path.Dir(name) == "tests/fixtures/consumer/clientcontracts/testdata" || path.Dir(name) == "tests/fixtures/consumer/clientcontracts/testdata/table-bindings") && !strings.HasSuffix(name, ".d.ts")
	}
	if !approved || strings.Contains(name, "_foundry.gen.") {
		return source{}, fmt.Errorf("source %q is outside approved handwritten fixture inputs", name)
	}
	// Reject aliases as well as escapes, so one file cannot acquire two owners.
	current := root
	for _, part := range strings.Split(name, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return source{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return source{}, fmt.Errorf("source %q contains a symlink", name)
		}
	}
	data, err := os.ReadFile(current)
	if err != nil {
		return source{}, err
	}
	result, err := parseSource(string(data))
	if ext == ".go" {
		result.language = "go"
	} else {
		result.language = "typescript"
	}
	return result, err
}

func parseSource(data string) (source, error) {
	lines := strings.SplitAfter(data, "\n")
	result := source{regions: make(map[string]excerpt)}
	var whole strings.Builder
	active := ""
	start := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		marker := regionMarker.FindStringSubmatch(trimmed)
		trailing := ""
		if marker == nil {
			if match := trailingMarker.FindStringSubmatch(strings.TrimSuffix(line, "\n")); match != nil {
				trailing = match[1]
				if strings.HasSuffix(line, "\n") {
					trailing += "\n"
				}
				marker = regionMarker.FindStringSubmatch(match[2])
			}
		}
		if marker == nil {
			if strings.HasPrefix(trimmed, "// doc-example:") {
				return source{}, fmt.Errorf("line %d: malformed region marker", i+1)
			}
			whole.WriteString(line)
			continue
		}
		name := marker[2]
		if marker[1] == "begin" {
			if active != "" {
				return source{}, fmt.Errorf("line %d: overlapping regions %s and %s", i+1, active, name)
			}
			if _, exists := result.regions[name]; exists {
				return source{}, fmt.Errorf("line %d: duplicate region %s", i+1, name)
			}
			active, start = name, i+1
			if trailing != "" {
				lines[i] = trailing
				start = i
				whole.WriteString(trailing)
			}
		} else {
			if active != name {
				return source{}, fmt.Errorf("line %d: unmatched region end %s", i+1, name)
			}
			text := strings.Join(lines[start:i], "")
			last := i
			if trailing != "" {
				text += trailing
				whole.WriteString(trailing)
				last = i + 1
			}
			result.regions[name] = excerpt{text: dedent(text), first: start + 1, last: last}
			active = ""
		}
	}
	if active != "" {
		return source{}, fmt.Errorf("unclosed region %s", active)
	}
	result.whole = excerpt{text: dedent(whole.String()), first: 1, last: len(lines)}
	return result, nil
}

// Remove the longest byte-identical space/tab prefix of all nonblank lines.
// Blank lines lose at most that prefix; no other whitespace is normalized.
func dedent(data string) string {
	lines := strings.SplitAfter(data, "\n")
	prefix := ""
	first := true
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if first {
			prefix, first = indent, false
		} else {
			n := 0
			for n < len(prefix) && n < len(indent) && prefix[n] == indent[n] {
				n++
			}
			prefix = prefix[:n]
		}
	}
	for i, line := range lines {
		n := 0
		for n < len(prefix) && n < len(line) && prefix[n] == line[n] {
			n++
		}
		lines[i] = line[n:]
	}
	return strings.Join(lines, "")
}

func renderFence(language, data string, finalNewline bool) string {
	width, run := 3, 0
	for _, c := range data {
		if c == '`' {
			run++
			if run >= width {
				width = run + 1
			}
		} else {
			run = 0
		}
	}
	delimiter := strings.Repeat("`", width)
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	rendered := delimiter + language + "\n" + data + delimiter
	if finalNewline {
		rendered += "\n"
	}
	return rendered
}
