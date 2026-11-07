// Command renderdocs renders compiled consumer regions in the existing guides.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/docsource"
)

func main() {
	check := flag.Bool("check", false, "compare without writing")
	root := flag.String("root", ".", "framework source root")
	var guides []string
	flag.Func("guide", "render only this guide basename (repeatable)", func(name string) error {
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".md") {
			return fmt.Errorf("guide must be a Markdown basename")
		}
		guides = append(guides, name)
		return nil
	})
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "renderdocs accepts flags only")
		os.Exit(1)
	}
	if err := run(*root, *check, guides...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(root string, check bool, guides ...string) error {
	documents, err := docsource.Load(root)
	if err != nil {
		return err
	}
	if len(guides) > 0 {
		selected := make([]docsource.Document, 0, len(guides))
		for _, name := range guides {
			found := false
			for _, document := range documents {
				if filepath.Base(document.Path) == name {
					selected = append(selected, document)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("unknown guide %q", name)
			}
		}
		documents = selected
	}
	if check {
		return docsource.Check(documents)
	}
	for _, document := range documents {
		if string(document.Original) == string(document.Rendered) {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, document.Path), document.Rendered, 0644); err != nil {
			return err
		}
	}
	return nil
}
