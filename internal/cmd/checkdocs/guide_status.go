package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	datedGuide       = regexp.MustCompile(`(?:19|20)[0-9]{2}-?[0-9]{2}-?[0-9]{2}`)
	pendingMilestone = regexp.MustCompile(`(?i)\b(?:remains?(?:\s+(?:in|part\s+of|separate))?|planned\s+in|arrives?\s+in)\s+\[?milestone\s+([0-9]{1,2})\b|\bmilestone\s+([0-9]{1,2})\s+(?:remains?\s+open|still\s+requires)\b`)
)

func completedMilestones(root string) (map[string]bool, error) {
	path := filepath.Join(root, "blueprint", "00-master-architecture-and-parity.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	completed := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		columns := strings.Split(line, "|")
		if len(columns) != 6 {
			continue
		}
		number := strings.TrimSpace(columns[1])
		if len(number) != 2 || number[0] < '0' || number[0] > '9' || number[1] < '0' || number[1] > '9' {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(columns[4]), "Complete") {
			completed[number] = true
		}
	}
	if len(completed) == 0 {
		return nil, fmt.Errorf("%s: missing completed milestone status rows", path)
	}
	return completed, nil
}

func currentGuide(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	relative = filepath.ToSlash(relative)
	return relative == "README.md" || strings.HasPrefix(relative, "docs/guides/") && !datedGuide.MatchString(filepath.Base(path))
}
