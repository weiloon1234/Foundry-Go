package main

import (
	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	datedGuide       = regexp.MustCompile(`(?:19|20)[0-9]{2}-?[0-9]{2}-?[0-9]{2}`)
	pendingMilestone = regexp.MustCompile(`(?i)\b(?:remains?(?:\s+(?:in|part\s+of|separate))?|planned\s+in|arrives?\s+in)\s+\[?milestone\s+([0-9]{1,2})\b|\bmilestone\s+([0-9]{1,2})\s+(?:remains?\s+open|still\s+requires)\b`)
)

func completedMilestones(root string, cache *markdown.Cache) (map[string]bool, error) {
	const name = "blueprint/00-master-architecture-and-parity.md"
	data, err := markdown.ReadFile(root, name, markdown.DocumentLimit(name))
	if err != nil {
		return nil, err
	}
	parsed, err := cache.Read(name, data)
	if err != nil {
		return nil, err
	}
	completed := make(map[string]bool)
	for _, row := range parsed.Blocks {
		// The status table has four columns: number, blueprint, requires, status.
		if row.Kind != markdown.TableRow || len(row.Cells) != 4 {
			continue
		}
		number := row.Cells[0]
		if len(number) != 2 || number[0] < '0' || number[0] > '9' || number[1] < '0' || number[1] > '9' {
			continue
		}
		if strings.HasPrefix(row.Cells[3], "Complete") {
			completed[number] = true
		}
	}
	if len(completed) == 0 {
		return nil, diagnostic.DocMilestone.At(filepath.Join(root, name), 0, 0).Say("master status table", "no completed milestone rows").To("restore the table with its current completion states").Err()
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
