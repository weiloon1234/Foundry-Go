package guideexamples_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

// Advanced-query guides reuse excerpts from compiled consumer packages. Keep
// the enclosing imports/declarations at the linked source instead of duplicating
// a second example implementation. Only indentation may differ in Markdown.
func TestGuideExcerpts(t *testing.T) {
	testkit.TrackExternalInputs(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate guide examples")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
	guides, err := filepath.Glob(filepath.Join(root, "docs/guides/*.md"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile("(?s)<!-- go-source: ([^\\n]+) -->\\n```go\\n(.*?)\\n```")
	legacy := regexp.MustCompile(`foundry:(model|projection)|\bquery\.[A-Z]|\b(?:models|reports)\.\w+Fields(?:At)?\(|\b\w+Draft\{|\bmodels\.Query\w+\(`)
	checked := 0
	for _, guide := range guides {
		data, err := os.ReadFile(guide)
		if err != nil {
			t.Fatal(err)
		}
		doc := string(data)
		if !strings.Contains(doc, "<!-- compiled-examples -->") {
			continue
		}
		checked++
		t.Run(filepath.Base(guide), func(t *testing.T) {
			if legacy.MatchString(doc) {
				t.Error("guide still teaches a generated model/query API; link orm-migration.md instead")
			}
			if !strings.Contains(doc, "(orm-migration.md)") {
				t.Error("guide needs the declaration migration link")
			}
			blocks := block.FindAllStringSubmatch(doc, -1)
			if len(blocks) != strings.Count(doc, "```go\n") {
				t.Fatal("every Go fence must reference its compiled source")
			}
			for _, example := range blocks {
				path, snippet := example[1], normalized(example[2])
				if !filepath.IsLocal(path) || !strings.HasPrefix(path, "tests/fixtures/consumer/") || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".gen.go") {
					t.Errorf("example must reference a handwritten consumer Go source: %s", path)
					continue
				}
				source, err := os.ReadFile(filepath.Join(root, path))
				if err != nil {
					t.Error(err)
					continue
				}
				if snippet == "" || !strings.Contains("\n"+normalized(string(source))+"\n", "\n"+snippet+"\n") {
					t.Errorf("guide excerpt differs from compiled source %s", path)
				}
			}
		})
	}
	if checked == 0 {
		t.Fatal("no compiled-example guides found")
	}
}

func normalized(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}
