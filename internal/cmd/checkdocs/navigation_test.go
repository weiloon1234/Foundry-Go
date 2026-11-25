package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitGuideFragments(t *testing.T) {
	for _, test := range []struct {
		name, source, target, wantError string
	}{
		{"same page", "[Read](#recipe-read)\n<a id=\"recipe-read\"></a>\n## Read\n", "", ""},
		{"other guide", "[Read](other.md#recipe-read)\n", "<a id=\"recipe-read\"></a>\n## Read\n", ""},
		{"encoded fragment", "[Read](other.md#recipe%2Dread)\n", "<a id=\"recipe-read\"></a>\n## Read\n", ""},
		{"entry", "[Tasks](other.md#agent-entry)\n", "<a id=\"agent-entry\"></a>\n## Agent entry\n", ""},
		{"missing same page", "[Read](#recipe-read)\n", "", `[anchor-missing] anchor "recipe-read"`},
		{"missing other page", "[Read](other.md#recipe-missing)\n", "<a id=\"recipe-read\"></a>\n## Read\n", `[anchor-missing] anchor "recipe-missing"`},
		{"heading is not an anchor", "[Tasks](other.md#agent-entry)\n", "## Agent entry\n", `[anchor-missing] anchor "agent-entry"`},
		{"fenced anchor", "[Read](other.md#recipe-read)\n", "````markdown\n```\n<a id=\"recipe-read\"></a>\n````\n", `[anchor-missing] anchor "recipe-read"`},
		{"duplicate anchor", "", "<a id=\"recipe-read\"></a>\n## Read\n<a id=\"recipe-read\"></a>\n## Read again\n", "[anchor-duplicate]"},
		{"malformed anchor", "", "<a id='recipe-read'></a>\n## Read\n", "[anchor-invalid]"},
		{"other fragments unchanged", "[Any](other.md#arbitrary-slug)\n[Local](#other)\n", "# Other\n", ""},
		{"external fragments unchanged", "[Remote](https://example.test/guide#recipe-read)\n", "", ""},
		{"code is not a link", "`[Read](#recipe-missing)`\n~~~markdown\n[Read](#recipe-missing)\n~~~\n", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testRepository(t)
			writeTestFile(t, root, "docs/guides/example.md", test.source)
			writeTestFile(t, root, "docs/guides/other.md", test.target)
			err := checkRepository(root)
			if test.wantError == "" && err != nil || test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("want %q, got %v", test.wantError, err)
			}
			data, err := os.ReadFile(filepath.Join(root, "docs/guides/example.md"))
			if err != nil || string(data) != test.source {
				t.Fatalf("check changed source: %q, %v", data, err)
			}
		})
	}
}

func TestAgentEntryLinkTargets(t *testing.T) {
	for _, test := range []struct {
		name, link, wantError string
	}{
		{"same page recipe", "#recipe-read", ""},
		{"other guide recipe", "other.md#recipe-read", ""},
		{"other guide entry", "other.md#agent-entry", ""},
		{"missing anchor", "other.md#recipe-missing", `[anchor-missing] anchor "recipe-missing"`},
		{"bare guide", "other.md", "must target a local guide's explicit"},
		{"implicit heading", "other.md#read", "must target a local guide's explicit"},
		{"other explicit ID", "other.md#background", "must target a local guide's explicit"},
		{"external entry", "https://example.test/other.md#agent-entry", "must target a local guide's explicit"},
		{"scheme relative", "//example.test/other.md#recipe-read", "must target a local guide's explicit"},
		{"nonguide", "../../README.md#recipe-read", "must target a local guide"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testRepository(t)
			writeTestFile(t, root, "docs/guides/example.md", "<a id=\"agent-entry\"></a>\n## Agent entry\n\n- Read: [Read]("+test.link+")\n\n<a id=\"recipe-read\"></a>\n## Read\n\n[Background](other.md#arbitrary-slug)\n")
			writeTestFile(t, root, "docs/guides/other.md", "<a id=\"agent-entry\"></a>\n## Agent entry\n\n<a id=\"recipe-read\"></a>\n## Read\n\n<a id=\"background\"></a>\n## Background\n")
			writeTestFile(t, root, "README.md", "<a id=\"recipe-read\"></a>\n# Read\n")
			err := checkRepository(root)
			if test.wantError == "" && err != nil || test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("want %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestRecipeFragmentFromOutsideGuides(t *testing.T) {
	root := testRepository(t)
	writeTestFile(t, root, "README.md", "[Read](docs/guides/example.md#recipe-read)\n")
	writeTestFile(t, root, "docs/guides/example.md", "## Read\n")
	if err := checkRepository(root); err == nil || !strings.Contains(err.Error(), `[anchor-missing] anchor "recipe-read"`) {
		t.Fatalf("missing guide anchor was not checked: %v", err)
	}
}

func TestDisplacedRecipeAnchor(t *testing.T) {
	root := testRepository(t)
	writeTestFile(t, root, "docs/guides/example.md", "<a id=\"agent-entry\"></a>\n## Agent entry\n- Read: [Read](#recipe-read)\n\n<a id=\"recipe-read\"></a>\n<!-- displaced -->\n## Read\n```go\nnotCompiled()\n```\n")
	if err := checkRepository(root); err == nil || !strings.Contains(err.Error(), "must be followed by a # heading") {
		t.Fatalf("displaced recipe escaped coverage: %v", err)
	}
}

func TestReferenceStyleNavigation(t *testing.T) {
	for _, test := range []struct{ name, text, want string }{
		{"missing target", "[Read][recipe]\n\n[recipe]: missing.md#recipe-read\n", "local link"},
		{"missing anchor", "[Read][recipe]\n\n[recipe]: other.md#recipe-missing\n", `[anchor-missing] anchor "recipe-missing"`},
		{"full", "[Read][RECIPE]\n\n[recipe]: other.md#recipe-read\n", ""},
		{"collapsed", "[Read][]\n\n[read]: other.md#recipe-read\n", ""},
		{"shortcut", "[Read]\n\n[read]: <other.md#recipe-read>\n", ""},
		{"same page", "[Read][recipe]\n\n[recipe]: #recipe-read\n<a id=\"recipe-read\"></a>\n## Read\n", ""},
		{"unused definition", "[recipe]: other.md#recipe-missing\n", ""},
		{"fenced use", "```markdown\n[Read][recipe]\n```\n[recipe]: other.md#recipe-missing\n", ""},
		{"inline code", "`[Read][recipe]`\n[recipe]: other.md#recipe-missing\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testRepository(t)
			writeTestFile(t, root, "docs/guides/example.md", test.text)
			writeTestFile(t, root, "docs/guides/other.md", "<a id=\"recipe-read\"></a>\n## Read\n")
			err := checkRepository(root)
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestFrameworkGuideDeclarations(t *testing.T) {
	for _, name := range []string{"AGENTS.md", "docs/guides/example.md", ".claude/skills/workflow/references/api.md"} {
		for _, test := range []struct{ text, want string }{
			{"Foundry guides: `missing#anchor`\n", "[guide-missing] guide missing"},
			{"Foundry guides: `other#missing`\n", `[anchor-missing] anchor "missing"`},
			{"Foundry guides: `other` trailing\n", "[guides-declaration] Foundry guides line"},
			{"Foundry guides: `other#recipe-read`\n", ""},
			{"```markdown\nFoundry guides: `missing` trailing\n```\n", ""},
		} {
			t.Run(name+test.text, func(t *testing.T) {
				root := testRepository(t)
				writeTestFile(t, root, name, test.text)
				writeTestFile(t, root, "docs/guides/other.md", "<a id=\"recipe-read\"></a>\n## Read\n")
				err := checkRepository(root)
				if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
					t.Fatalf("want %q, got %v", test.want, err)
				}
			})
		}
	}
}

func TestRepositorySkillsReceiveRetirementCheck(t *testing.T) {
	root := testRepository(t)
	writeTestFile(t, root, ".claude/skills/example/SKILL.md", "Use orm.DoesntHave.\n")
	err := checkRepository(root)
	if err == nil || !strings.Contains(err.Error(), ".claude/skills/example/SKILL.md:1:") || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("skill bypassed retirement: %v", err)
	}
}

func TestQuotedFenceDoesNotHideFollowingLinks(t *testing.T) {
	root := testRepository(t)
	writeTestFile(t, root, "README.md", "> ```text\n> [Hidden](hidden.md)\n\n[Missing](missing.md)\n")
	err := checkRepository(root)
	if err == nil || !strings.Contains(err.Error(), `local link "missing.md"`) || strings.Contains(err.Error(), `local link "hidden.md"`) {
		t.Fatalf("wrong container: %v", err)
	}
}

func TestRepositoryCommentsAreNotTeaching(t *testing.T) {
	root := testRepository(t)
	writeTestFile(t, root, "docs/guides/example.md", "<!--\n<pre>\n```go\nvar example = models.UserDraft{}\nrows.Count(query)\n```\n[Missing](missing.md)\n</pre>\n-->\n")
	if err := checkRepository(root); err != nil {
		t.Fatal(err)
	}
}
