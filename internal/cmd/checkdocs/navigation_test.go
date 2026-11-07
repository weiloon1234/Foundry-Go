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
		{"missing same page", "[Read](#recipe-read)\n", "", `explicit anchor "recipe-read" is absent`},
		{"missing other page", "[Read](other.md#recipe-missing)\n", "<a id=\"recipe-read\"></a>\n## Read\n", `explicit anchor "recipe-missing" is absent`},
		{"heading is not an anchor", "[Tasks](other.md#agent-entry)\n", "## Agent entry\n", `explicit anchor "agent-entry" is absent`},
		{"fenced anchor", "[Read](other.md#recipe-read)\n", "````markdown\n```\n<a id=\"recipe-read\"></a>\n````\n", `explicit anchor "recipe-read" is absent`},
		{"duplicate anchor", "", "<a id=\"recipe-read\"></a>\n## Read\n<a id=\"recipe-read\"></a>\n## Read again\n", "duplicate explicit anchor"},
		{"malformed anchor", "", "<a id='recipe-read'></a>\n## Read\n", "invalid explicit anchor"},
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
		{"missing anchor", "other.md#recipe-missing", `explicit anchor "recipe-missing" is absent`},
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
	if err := checkRepository(root); err == nil || !strings.Contains(err.Error(), `explicit anchor "recipe-read" is absent`) {
		t.Fatalf("missing guide anchor was not checked: %v", err)
	}
}
