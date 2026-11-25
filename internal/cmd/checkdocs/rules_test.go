package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
)

// ruleRow is the table row of a rule with the given teaching tokens.
func ruleRow(rule diagnostic.Rule) string {
	application := ""
	if rule.Application != "" {
		application = "`" + rule.ApplicationReference() + "`"
	}
	return "| `" + rule.ID + "` | check | `" + rule.Reference() + "` | " + application + " |"
}

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
	listing := "<a id=\"diagnostic-rules\"></a>\n## Rules\n\n| Rule | Check | Teaching | In an application |\n| --- | --- | --- | --- |\n"
	for _, rule := range diagnostic.Rules() {
		if anchors[rule.Guide] == nil {
			anchors[rule.Guide] = map[string]bool{}
		}
		anchors[rule.Guide][rule.Anchor] = true
		if rule.Application != "" {
			anchors[rule.Guide][rule.Application] = true
		}
		if row == nil {
			listing += ruleRow(rule) + "\n"
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
		return ruleRow(rule)
	})
	if err := checkRules(root); err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule raw-sql: not listed in the tooling guide") {
		t.Fatalf("an unlisted rule passed: %v", err)
	}
}

func TestRuleTableMustMatchTheRulesBothWays(t *testing.T) {
	correct := ruleRow

	root := ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "model-table" {
			return "| `model-table` | models check | `flat-routes#staged-routes` |  |"
		}
		return correct(rule)
	})
	err := checkRules(root)
	if err == nil || !strings.Contains(err.Error(), "[rule-catalogue] rule model-table: table teaches flat-routes#staged-routes, rule teaches model-declarations#recipe-declare-table") || strings.Contains(err.Error(), "rule raw-sql") {
		t.Fatalf("a wrong teaching column passed or a correct row failed: %v", err)
	}

	root = ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "rule-catalogue" {
			return correct(rule) + "\n| `deleted-rule` | check | `flat-routes#staged-routes` |  |"
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

func TestRuleTableMustStateTheApplicationTeaching(t *testing.T) {
	// doc-binding teaches a different recipe in an application.
	root := ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "doc-binding" {
			return "| `doc-binding` | check | `" + rule.Reference() + "` |  |"
		}
		return ruleRow(rule)
	})
	err := checkRules(root)
	if err == nil || !strings.Contains(err.Error(), `rule doc-binding: table teaches "" in an application, rule teaches "developer-tooling-and-testing#recipe-application-documents"`) {
		t.Fatalf("a row without the application teaching passed: %v", err)
	}

	// A rule without an application anchor must leave the column empty.
	root = ruleRepository(t, func(rule diagnostic.Rule) string {
		if rule.ID == "raw-sql" {
			return "| `raw-sql` | check | `" + rule.Reference() + "` | `flat-routes#staged-routes` |"
		}
		return ruleRow(rule)
	})
	err = checkRules(root)
	if err == nil || !strings.Contains(err.Error(), `rule raw-sql: table teaches "flat-routes#staged-routes" in an application, rule teaches ""`) {
		t.Fatalf("an invented application teaching passed: %v", err)
	}

	// An application anchor the guide does not declare is refused.
	root = ruleRepository(t, nil)
	guide := filepath.Join(root, "docs/guides/developer-tooling-and-testing.md")
	data, readErr := os.ReadFile(guide)
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := strings.ReplaceAll(string(data), "<a id=\"recipe-application-documents\"></a>\n", "")
	if err := os.WriteFile(guide, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	err = checkRules(root)
	if err == nil || !strings.Contains(err.Error(), "rule doc-binding: names application anchor recipe-application-documents, which guide developer-tooling-and-testing does not declare") {
		t.Fatalf("a missing application anchor passed: %v", err)
	}
}
