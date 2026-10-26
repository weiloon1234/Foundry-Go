package retirement

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

var beforeMarker = regexp.MustCompile(`^<!-- orm-example: [a-z0-9-]+-before -->$`)

// CheckDocument rejects retired spellings in active documentation. Only labelled,
// fenced before examples in the migration guide describe the removed API.
func CheckDocument(name string, data []byte) error {
	denied, err := declarations()
	if err != nil {
		return err
	}
	patterns := []*regexp.Regexp{regexp.MustCompile(`database/query\b`), regexp.MustCompile(`\bquery\.[A-Z]\w*`), regexp.MustCompile(`\bLegacy[A-Z]\w*`)}
	for owner, names := range denied {
		for _, name := range names {
			patterns = append(patterns, regexp.MustCompile(`\b`+regexp.QuoteMeta(path.Base(owner)+"."+name)+`\b`))
		}
	}
	var failures []error
	pending, before := false, false
	fence := ""
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if fence == "" && beforeMarker.MatchString(trimmed) {
			pending = name == "docs/guides/orm-migration.md"
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
				before = pending
				pending = false
			} else if fence == marker {
				fence = ""
				before = false
			}
			continue
		}
		if before {
			continue
		}
		pending = false
		for _, pattern := range patterns {
			if pattern.MatchString(line) {
				failures = append(failures, fmt.Errorf("%s:%d: retired API outside a migration before block", name, i+1))
				break
			}
		}
	}
	return errors.Join(failures...)
}
