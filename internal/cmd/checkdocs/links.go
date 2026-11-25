package main

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
)

func checkExplicitFragment(pages *docrefs.Pages, source string, line int, target, fragment string) error {
	anchors, err := pages.Load(target)
	if err != nil {
		return err
	}
	if err := anchors.CheckFragment(source, line, fragment); err != nil {
		return fmt.Errorf("%w (target %s)", err, target)
	}
	return nil
}
