package guideadapters_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

// Each marked excerpt belongs to a compiled fixture. Keep the guide text in sync
// with that source instead of maintaining a second, unchecked Go example.
func TestGuideSnippets(t *testing.T) {
	testkit.TrackExternalInputs(t)
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate guide examples")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../../../.."))
	if !filepath.IsAbs(root) {
		if wd, err := os.Getwd(); err == nil {
			for dir := wd; ; dir = filepath.Dir(dir) {
				if _, err := os.Stat(filepath.Join(dir, "docs/guides")); err == nil {
					root = dir
					break
				}
				if parent := filepath.Dir(dir); parent == dir {
					break
				}
			}
		}
	}
	guides, err := filepath.Glob(filepath.Join(root, "docs/guides/*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(guides) == 0 {
		t.Fatal("no guide markdown files found")
	}
	complete := map[string]bool{
		"model-references.md": true, "model-extensions.md": true,
		"model-extension-slots.md": true, "attachments.md": true,
		"http-model-binding.md": true, "scoped-model-binding.md": true,
		"model-pagination.md": true, "http-pagination.md": true,
		"http-cursor-pagination.md": true, "model-scaffolding.md": true,
	}
	pattern := regexp.MustCompile("(?s)<!-- compiled-source: ([^\\n]+) -->\\n```go\\n(.*?)\\n```")
	for _, guide := range guides {
		data, err := os.ReadFile(guide)
		if err != nil {
			t.Fatal(err)
		}
		blocks := pattern.FindAllStringSubmatch(string(data), -1)
		if complete[filepath.Base(guide)] && len(blocks) != strings.Count(string(data), "```go\n") {
			t.Errorf("%s has an unlinked Go example", filepath.Base(guide))
		}
		for _, block := range blocks {
			if !strings.HasPrefix(block[1], "tests/fixtures/") || filepath.Ext(block[1]) != ".go" || strings.Contains(block[1], "..") {
				t.Fatalf("invalid compiled fixture path %s", block[1])
			}
			source, err := os.ReadFile(filepath.Join(root, block[1]))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(source), strings.TrimSpace(block[2])) {
				t.Errorf("%s differs from compiled source %s", filepath.Base(guide), block[1])
			}
		}
	}
}
