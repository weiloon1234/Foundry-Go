package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
)

const rulesSource = "internal/diagnostic/rule.go"

// tableRow is one `| `rule` | `guide#anchor` | `guide#anchor` or empty |` row of
// the tooling guide's rule table: the teaching token, then the token an application
// finding teaches when it differs.
var tableRow = regexp.MustCompile("^\\| `([a-z0-9-]+)` \\| `([a-z0-9-]+#[a-z0-9-]+)` \\| (?:`([a-z0-9-]+#[a-z0-9-]+)`)? \\|$")

// teaching is what one table row says a rule teaches.
type teaching struct{ framework, application string }

// listedRules reads the rule table of the tooling guide: rule ID to its teaching
// tokens, in the order written, and the IDs listed more than once.
func listedRules(guide string) (rows map[string]teaching, repeated []string) {
	rows = map[string]teaching{}
	inRules := false
	for _, line := range strings.Split(guide, "\n") {
		switch {
		case strings.HasPrefix(line, "<a id=\""):
			inRules = line == `<a id="`+diagnostic.RuleCatalogue.Anchor+`"></a>`
		case inRules:
			if match := tableRow.FindStringSubmatch(line); match != nil {
				if _, seen := rows[match[1]]; seen {
					repeated = append(repeated, match[1])
				}
				rows[match[1]] = teaching{match[2], match[3]}
			}
		}
	}
	return rows, repeated
}

// checkRules requires every diagnostic rule to name an existing guide and explicit
// anchor, and the tooling guide's rule table to list exactly the rules, each with
// its own teaching tokens: a finding must always point at a page that teaches its fix.
func checkRules(root string) error {
	pages := docrefs.NewPages(root, nil)
	listing, err := markdown.ReadFile(root, "docs/guides/"+diagnostic.RuleCatalogue.Guide+".md", markdown.MaxDocumentBytes)
	if err != nil {
		return err
	}
	rows, repeated := listedRules(string(listing))
	var failures []error
	finding := diagnostic.RuleCatalogue.At(rulesSource, 0, 0)
	known := map[string]bool{}
	for _, rule := range diagnostic.Rules() {
		known[rule.ID] = true
		subject := "rule " + rule.ID
		anchors, err := pages.Load(filepath.Join(root, "docs", "guides", rule.Guide+".md"))
		switch {
		case err != nil:
			failures = append(failures, finding.Say(subject, fmt.Sprintf("names guide %s, which cannot be read", rule.Guide)).To("name a guide in docs/guides").Err())
		case anchors.CheckFragment(rulesSource, 0, "guide "+rule.Guide, rule.Anchor) != nil:
			failures = append(failures, finding.Say(subject, fmt.Sprintf("names anchor %s, which guide %s does not declare", rule.Anchor, rule.Guide)).To("add the explicit anchor to the guide or correct the rule").Err())
		}
		if rule.Application != "" && err == nil && anchors.CheckFragment(rulesSource, 0, "guide "+rule.Guide, rule.Application) != nil {
			failures = append(failures, finding.Say(subject, fmt.Sprintf("names application anchor %s, which guide %s does not declare", rule.Application, rule.Guide)).To("add the explicit anchor to the guide or correct the rule").Err())
		}
		wantApplication := ""
		if rule.Application != "" {
			wantApplication = rule.ApplicationReference()
		}
		switch row, listed := rows[rule.ID]; {
		case !listed:
			failures = append(failures, finding.Say(subject, "not listed in the tooling guide").To("add the rule to the diagnostic rules table").Err())
		case row.framework != rule.Reference():
			failures = append(failures, finding.Say(subject, fmt.Sprintf("table teaches %s, rule teaches %s", row.framework, rule.Reference())).To("make the table row name the rule's guide anchor").Err())
		case row.application != wantApplication:
			failures = append(failures, finding.Say(subject, fmt.Sprintf("table teaches %q in an application, rule teaches %q", row.application, wantApplication)).To("make the table's last column name the rule's application anchor, or leave it empty").Err())
		}
	}
	for _, id := range sortedKeys(rows) {
		if !known[id] {
			failures = append(failures, finding.Say("rule "+id, "listed in the tooling guide but not defined").To("remove the row from the diagnostic rules table").Err())
		}
	}
	for _, id := range repeated {
		failures = append(failures, finding.Say("rule "+id, "listed twice in the tooling guide").To("keep one row in the diagnostic rules table").Err())
	}
	return errors.Join(failures...)
}

func sortedKeys(m map[string]teaching) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
