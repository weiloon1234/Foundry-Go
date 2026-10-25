package oracle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Selection follows Go's slash-separated test regex syntax. Expected records
// come only from the frozen inventory and flags, never from observed callbacks.
type Selection struct {
	run, skip [][]*regexp.Regexp
	disabled  []string
}

func SelectTests(run, skip string) (Selection, error) {
	r, err := testPatterns(run)
	if err != nil {
		return Selection{}, err
	}
	var s [][]*regexp.Regexp
	if skip != "" {
		s, err = testPatterns(skip)
	}
	return Selection{run: r, skip: s}, err
}

func testPatterns(pattern string) ([][]*regexp.Regexp, error) {
	var alternatives [][]*regexp.Regexp
	var path []*regexp.Regexp
	brackets, parens, start := 0, 0, 0
	appendPart := func(end int) error {
		var normalized strings.Builder
		for _, r := range pattern[start:end] {
			switch {
			case unicode.IsSpace(r):
				normalized.WriteByte('_')
			case !strconv.IsPrint(r):
				q := strconv.QuoteRune(r)
				normalized.WriteString(q[1 : len(q)-1])
			default:
				normalized.WriteRune(r)
			}
		}
		re, err := regexp.Compile(normalized.String())
		if err != nil {
			return fmt.Errorf("oracle test selection: %w", err)
		}
		path = append(path, re)
		return nil
	}
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			i++
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			}
		case '(':
			if brackets == 0 {
				parens++
			}
		case ')':
			if brackets == 0 {
				parens--
			}
		case '/', '|':
			if brackets != 0 || parens != 0 {
				continue
			}
			if err := appendPart(i); err != nil {
				return nil, err
			}
			if pattern[i] == '|' {
				alternatives = append(alternatives, path)
				path = nil
			}
			start = i + 1
		}
	}
	if err := appendPart(len(pattern)); err != nil {
		return nil, err
	}
	return append(alternatives, path), nil
}

func matchesTest(patterns [][]*regexp.Regexp, name []string) (match, partial bool) {
	for _, pattern := range patterns {
		ok := true
		for i, part := range name {
			if i == len(pattern) {
				break
			}
			if !pattern[i].MatchString(part) {
				ok = false
				break
			}
		}
		if ok {
			return true, len(name) < len(pattern)
		}
	}
	return false, false
}

func (s Selection) Includes(test string) bool {
	for _, root := range s.disabled {
		if test == root || strings.HasPrefix(test, root+"/") {
			return false
		}
	}
	parts := strings.Split(test, "/")
	if ok, _ := matchesTest(s.run, parts); !ok {
		return false
	}
	// A skipped ancestor prevents all its descendants from running, even if a
	// different alternative would only partially match the full descendant path.
	for n := 1; n <= len(parts); n++ {
		if ok, partial := matchesTest(s.skip, parts[:n]); ok && !partial {
			return false
		}
	}
	return true
}

func recordTest(id, prefix, site string) (string, error) {
	name, ok := strings.CutPrefix(id, prefix+"/")
	if !ok {
		return "", fmt.Errorf("oracle record has wrong package: %s", id)
	}
	split := strings.LastIndexByte(name, '/')
	if split < 0 {
		return "", fmt.Errorf("oracle record lacks occurrence: %s", id)
	}
	if n, err := strconv.Atoi(name[split+1:]); err != nil || n < 0 {
		return "", fmt.Errorf("oracle record has invalid occurrence: %s", id)
	}
	name, ok = strings.CutSuffix(name[:split], "/"+site)
	if !ok || name == "" {
		return "", fmt.Errorf("oracle record lacks test identity: %s", id)
	}
	return name, nil
}
