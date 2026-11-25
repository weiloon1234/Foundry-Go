package main

import "github.com/weiloon1234/Foundry-Go/internal/docrefs"

func checkExplicitFragment(pages *docrefs.Pages, source string, line int, target, page, fragment string) error {
	anchors, err := pages.Load(target)
	if err != nil {
		return err
	}
	return anchors.CheckFragment(source, line, page, fragment)
}

// checkLinkFragment validates the fragment of an ordinary link to a Markdown document:
// an explicit anchor, or a heading a viewer resolves.
func checkLinkFragment(pages *docrefs.Pages, source string, line int, target, page, fragment string) error {
	anchors, err := pages.Load(target)
	if err != nil {
		return err
	}
	return anchors.CheckLink(source, line, page, fragment)
}
