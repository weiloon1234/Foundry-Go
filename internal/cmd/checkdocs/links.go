package main

import "github.com/weiloon1234/Foundry-Go/internal/docrefs"

func checkExplicitFragment(pages *docrefs.Pages, source string, line int, target, page, fragment string) error {
	anchors, err := pages.Load(target)
	if err != nil {
		return err
	}
	return anchors.CheckFragment(source, line, page, fragment)
}
