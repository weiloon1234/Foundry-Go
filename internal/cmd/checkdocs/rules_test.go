package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
)

// ruleRepository writes one guide per rule guide, declaring every anchor its rules
// name, and a rule table in the tooling guide with the row each rule gets from row
// (an empty row is omitted). A nil row writes the correct row for every rule.
func ruleRepository(t *testing.T, row func(diagnostic.Rule) string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]map[string]bool{}
	listing := "<a id=\"diagnostic-rules\"></a>\n## Rules\n\n| Rule | Check | Teaching |\n| --- | --- | --- |\n"
	for _, rule := range diagnostic.Rules() {
		if anchors[rule.Guide] == nil {
			anchors[rule.Guide] = map[string]bool{}
		}
		anchors[rule.Guide][rule.Anchor] = true
		if row == nil {
			listing += "| `" + rule.ID + "` | check | `" + rule.Reference() + "` |\n"
		} else if text := row(rule); text != "" {
			listing += text + "\n"
		}
	}
	for guide, ids := range anchors {
		text := ""
		if guide == diagnostic.RuleCatalogue.Guide {
			text = listing + "\n"
		}
		for id := range ids {
			if guide == diagnostic.RuleCatalogue.Guide && id == diagnostic.RuleCatalogue.Anchor {
				continue
			}
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

	root = ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "raw-sql" {
			return ""
		}
		return "| `" + rule.ID + "` | check | `" + rule.Reference() + "` |"
	})
	if err := checkRules(root); err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule raw-sql: not listed in the tooling guide") {
		t.Fatalf("an unlisted rule passed: %v", err)
	}
}

func TestRuleTableMustMatchTheRulesBothWays(t *testing.T) {
	correct := func(rule diagnostic.Rule) string {
		return "| `" + rule.ID + "` | check | `" + rule.Reference() + "` |"
	}

	root := ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "model-table" {
			return "| `model-table` | models check | `flat-routes#staged-routes` |"
		}
		return correct(rule)
	})
	err := checkRules(root)
	if err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule model-table: table teaches flat-routes#staged-routes, rule teaches model-declarations#recipe-declare-table") || strings.Contains(err.Error(), "rule raw-sql") {
		t.Fatalf("a wrong teaching column passed or a correct row failed: %v", err)
	}

	root = ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "rule-catalogue" {
			return correct(rule) + "\n| `deleted-rule` | check | `flat-routes#staged-routes` |"
		}
		return correct(rule)
	})
	if err := checkRules(root); err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule deleted-rule: listed in the tooling guide but not defined") {
		t.Fatalf("a row for a rule that no longer exists passed: %v", err)
	}

	root = ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "raw-sql" {
			return correct(rule) + "\n" + correct(rule)
		}
		return correct(rule)
	})
	if err := checkRules(root); err == nil || !strings.Contains(err.Error(), "rule raw-sql: listed twice") {
		t.Fatalf("a repeated row passed: %v", err)
	}

	root = ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "raw-sql" {
			return "An ID in prose, `raw-sql`, is not a table row."
		}
		return correct(rule)
	})
	if err := checkRules(root); err == nil || !strings.Contains(err.Error(), "rule raw-sql: not listed in the tooling guide") {
		t.Fatalf("an ID mentioned in prose passed as a row: %v", err)
	}
}
