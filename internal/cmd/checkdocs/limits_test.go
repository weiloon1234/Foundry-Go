package main

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
)

func filler(bytes int) string {
	return "# Record\n\n" + strings.Repeat("Ordinary historical text line.\n", bytes/31)
}

func TestRepositoryAppliesOneSizeRulePerPath(t *testing.T) {
	tests := []struct {
		name, path, content, want string
	}{
		{"evidence above the guide limit is a record", "docs/evidence/big.md", filler(markdown.MaxDocumentBytes + 100<<10), ""},
		{"changelog above the guide limit is a record", "CHANGELOG.md", filler(markdown.MaxDocumentBytes + 100<<10), ""},
		{"evidence above the record limit", "docs/evidence/huge.md", filler(markdown.MaxRecordBytes + 1<<10), "docs/evidence/huge.md: [doc-limit] input: over 1572864 bytes"},
		{"guide above the guide limit", "docs/guides/big.md", filler(markdown.MaxDocumentBytes + 1<<10), "docs/guides/big.md: [doc-limit] input: over 524288 bytes"},
		{"skill above the guide limit", ".claude/skills/x/SKILL.md", filler(markdown.MaxDocumentBytes + 1<<10), ".claude/skills/x/SKILL.md: [doc-limit] input: over 524288 bytes"},
		{"blueprint above the guide limit", "blueprint/02-big.md", filler(markdown.MaxDocumentBytes + 1<<10), "blueprint/02-big.md: [doc-limit] input: over 524288 bytes"},
		{"deep nesting in a record", "docs/evidence/deep.md", strings.Repeat("> ", 17) + "x\n", "docs/evidence/deep.md:1: [doc-limit] line: opens with more than 16 quote or list markers"},
		{"CR-only endings", "docs/guides/cr.md", "# T\r\rText\r", "docs/guides/cr.md:1: [doc-limit] line ending: a lone CR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testRepository(t)
			writeTestFile(t, root, test.path, test.content)
			err := checkRepository(root)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestCompletedMilestonesReadsTableCellsOnly(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "blueprint/00-master-architecture-and-parity.md", strings.Join([]string{
		"# Master",
		"",
		"```text",
		"| 07 | Fenced | None | Complete |",
		"```",
		"",
		"| Milestone | Blueprint | Depends on | Status |",
		"| --- | --- | --- | --- |",
		"| 01 | [Core](01.md) | 00 | **Complete** - verified |",
		"| 02 | Pipes `a\\|b` | 01 | Complete |",
		"| 03 | Open | 02 | In progress |",
		"| 4 | Short number | 03 | Complete |",
		"",
		"| 09 | Not a table row, only text | x | Complete |",
		"",
	}, "\n"))
	completed, err := completedMilestones(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 2 || !completed["01"] || !completed["02"] {
		t.Fatalf("completed = %v, want exactly 01 and 02", completed)
	}
}
