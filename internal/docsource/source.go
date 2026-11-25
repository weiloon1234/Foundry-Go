// Package docsource renders guide examples from compiled consumer source.
package docsource

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var sourceLine = regexp.MustCompile(`^line (\d+): (.*)$`)

var regionMarker = regexp.MustCompile(`^// doc-example:(begin|end) ([a-z0-9]+(?:-[a-z0-9]+)*)$`)

type excerpt struct {
	text        string
	first, last int
}

type source struct {
	bytes    int
	language string
	whole    excerpt
	regions  map[string]excerpt
}

func readSource(policy Policy, root, document, name string) (source, error) {
	language, err := policy.approve(root, document, name)
	if err != nil {
		return source{}, err
	}
	return loadSource(policy, root, name, language)
}

// loadSource reads an approved file and parses its regions.
func loadSource(policy Policy, root, name, language string) (source, error) {
	data, err := markdown.ReadFile(root, name, markdown.MaxSourceBytes)
	if err != nil {
		return source{}, err
	}
	if policy.words().application && (language == "typescript" || language == "tsx") && bytes.Contains(data, []byte("@ts-nocheck")) {
		return source{}, safeerror.New(fmt.Sprintf("source %q is not bindable: it contains @ts-nocheck, which would hide the file from the application's type-check; remove it or bind another file", name))
	}
	parsed, err := parseSource(string(data), language)
	if err != nil {
		line, message := 0, err.Error()
		if match := sourceLine.FindStringSubmatch(message); match != nil {
			line, _ = strconv.Atoi(match[1])
			message = match[2]
		}
		return source{}, finding(policy, diagnostic.DocRegion, name, line).Text("", message).Err()
	}
	parsed.bytes = len(data)
	return parsed, nil
}

func parseSource(data, language string) (source, error) {
	locations := markdown.NewLines([]byte(data))
	comments, elements := lineComments(data, language)
	for offset := 0; offset < len(data); {
		n := strings.Index(data[offset:], "// doc-example:")
		if n < 0 {
			break
		}
		offset += n
		for _, element := range elements {
			if offset >= element.start && offset < element.end {
				return source{}, safeerror.New(fmt.Sprintf("line %d: region marker sits inside JSX, where a line comment cannot start a region; wrap whole declarations with the markers (a component or hook function, or statements before its return).", locations.Number(offset)))
			}
		}
		if !comments[offset] {
			return source{}, safeerror.New(fmt.Sprintf("line %d: region marker must be an actual line comment, outside literals and block comments; move the marker outside the literal or block comment.", locations.Number(offset)))
		}
		offset += len("// doc-example:")
	}
	lines := strings.SplitAfter(data, "\n")
	result := source{language: language, regions: make(map[string]excerpt)}
	var whole strings.Builder
	active := ""
	start := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		marker := regionMarker.FindStringSubmatch(trimmed)
		if marker == nil {
			if strings.Contains(trimmed, "// doc-example:") || strings.HasPrefix(trimmed, "// migration:") || strings.HasPrefix(trimmed, "// guide:") {
				return source{}, safeerror.New(fmt.Sprintf("line %d: region marker must use a standalone // doc-example:begin or // doc-example:end comment; put each marker alone on a line.", i+1))
			}
			whole.WriteString(line)
			continue
		}
		name := marker[2]
		if marker[1] == "begin" {
			if active != "" {
				return source{}, safeerror.New(fmt.Sprintf("line %d: overlapping regions %s and %s; close the first region before starting another.", i+1, active, name))
			}
			if _, exists := result.regions[name]; exists {
				return source{}, safeerror.New(fmt.Sprintf("line %d: duplicate region %s; give the second region a different name.", i+1, name))
			}
			active, start = name, i+1
		} else {
			if active != name {
				return source{}, safeerror.New(fmt.Sprintf("line %d: unmatched region end %s; match its name to an open begin marker.", i+1, name))
			}
			firstBoundary, lastBoundary := start, i-1
			for firstBoundary <= lastBoundary && strings.TrimSpace(lines[firstBoundary]) == "" {
				firstBoundary++
			}
			for lastBoundary >= firstBoundary && strings.TrimSpace(lines[lastBoundary]) == "" {
				lastBoundary--
			}
			if firstBoundary <= lastBoundary {
				for _, boundary := range []int{firstBoundary, lastBoundary} {
					if strings.TrimSpace(lines[boundary]) == "//" {
						return source{}, safeerror.New(fmt.Sprintf("line %d: region %s begins or ends with a bare comment separator; move the region marker outside the comment group, or include the whole comment.", boundary+1, name))
					}
				}
			}
			text := strings.Join(lines[start:i], "")
			last := i
			result.regions[name] = excerpt{text: dedent(trimRegionBlankLines(text)), first: start + 1, last: last}
			active = ""
		}
	}
	if active != "" {
		return source{}, safeerror.New(fmt.Sprintf("line %d: unclosed region %s; add its matching end marker.", start, active))
	}
	result.whole = excerpt{text: dedent(whole.String()), first: 1, last: len(lines)}
	return result, nil
}

// Region boundary padding separates marker and declaration comment groups.
// Whole-file inclusions and interior blank lines retain their existing layout.
func trimRegionBlankLines(data string) string {
	lines := strings.SplitAfter(data, "\n")
	first, end := 0, len(lines)
	for first < end && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	for end > first && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[first:end], "")
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
