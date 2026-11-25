package main

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
)

func checkExplicitFragment(pages *docrefs.Pages, source string, line int, target, fragment string) error {
	anchors, err := pages.Load(target)
	if err != nil {
		return err
	}
	if err := anchors.CheckFragment(source, line, fragment); err != nil {
		found, _ := diagnostic.Findings(err)
		if len(found) != 1 {
			return err
		}
		finding := found[0]
		finding.Subject = fmt.Sprintf("%s (target %s)", finding.Subject, target)
		return finding.Err()
	}
	return nil
}
