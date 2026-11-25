package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
)

// ruleRepository writes one guide per rule guide, declaring every anchor its rules
// name, and lists every rule ID in the tooling guide.
func ruleRepository(t *testing.T, skip func(diagnostic.Rule) bool) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]map[string]bool{}
	listing := ""
	for _, rule := range diagnostic.Rules() {
		if anchors[rule.Guide] == nil {
			anchors[rule.Guide] = map[string]bool{}
		}
		anchors[rule.Guide][rule.Anchor] = true
		if skip == nil || !skip(rule) {
			listing += "| `" + rule.ID + "` |\n"
		}
	}
	for guide, ids := range anchors {
		text := listing
		for id := range ids {
			text += "<a id=\"" + id + "\"></a>\n## Heading " + id + "\n\n"
		}
		writeTestFile(t, root, "docs/guides/"+guide+".md", text)
	}
	return root
}

func TestRuleCatalogueMatchesTheRealGuides(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRules(root); err != nil {
		t.Fatal(err)
	}
}

func TestEveryRuleNamesAnExistingGuideAnchorAndIsListed(t *testing.T) {
	if err := checkRules(ruleRepository(t, nil)); err != nil {
		t.Fatal(err)
	}

	root := ruleRepository(t, nil)
	if err := os.Remove(filepath.Join(root, "docs/guides/flat-routes.md")); err != nil {
		t.Fatal(err)
	}
	if err := checkRules(root); err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule route-nesting: names guide flat-routes, which cannot be read") {
		t.Fatalf("a rule naming a missing guide passed: %v", err)
	}

	root = ruleRepository(t, nil)
	writeTestFile(t, root, "docs/guides/model-declarations.md", "<a id=\"recipe-declare-table\"></a>\n## Declare\n")
	err := checkRules(root)
	if err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule generated-stale: names anchor recipe-stale-declarations, which guide model-declarations does not declare") || strings.Contains(err.Error(), "rule model-table:") {
		t.Fatalf("a rule naming a missing anchor passed or a present anchor failed: %v", err)
	}

	root = ruleRepository(t, func(rule diagnostic.Rule) bool { return rule.ID == "raw-sql" })
	if err := checkRules(root); err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule raw-sql: not listed in the tooling guide") {
		t.Fatalf("an unlisted rule passed: %v", err)
	}
}
