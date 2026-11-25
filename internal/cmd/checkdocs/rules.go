package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
)

const rulesSource = "internal/diagnostic/rule.go"

// checkRules requires every diagnostic rule to name an existing guide and explicit
// anchor, and the tooling guide to list the rule's ID: a finding must always point
// at a page that teaches its fix.
func checkRules(root string) error {
	pages := docrefs.NewPages(root, nil)
	listing, err := markdown.ReadFile(root, "docs/guides/"+diagnostic.DocRendered.Guide+".md", markdown.MaxDocumentBytes)
	if err != nil {
		return err
	}
	var failures []error
	for _, rule := range diagnostic.Rules() {
		finding := diagnostic.RuleCatalogue.At(rulesSource, 0, 0)
		subject := "rule " + rule.ID
		anchors, err := pages.Load(filepath.Join(root, "docs", "guides", rule.Guide+".md"))
		switch {
		case err != nil:
			failures = append(failures, finding.Say(subject, fmt.Sprintf("names guide %s, which cannot be read", rule.Guide)).To("name a guide in docs/guides").Err())
		case anchors.CheckFragment(rulesSource, 0, rule.Anchor) != nil:
			failures = append(failures, finding.Say(subject, fmt.Sprintf("names anchor %s, which guide %s does not declare", rule.Anchor, rule.Guide)).To("add the explicit anchor to the guide or correct the rule").Err())
		}
		if !strings.Contains(string(listing), "`"+rule.ID+"`") {
			failures = append(failures, finding.Say(subject, "not listed in the tooling guide").To("add the rule to the diagnostic rules table").Err())
		}
	}
	return errors.Join(failures...)
}
