package migration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

// The guide includes exact regions of the compiled after examples. Every Go block
// needs a region ID; missing, duplicate and stale snippets fail this test.
func TestGuideSnippets(t *testing.T) {
	root, regions := compiledRegions(t, "migration")
	used := checkGuideSnippets(t, filepath.Join(root, "docs/guides/orm-migration.md"), regions)
	for name := range regions {
		if !used[name] {
			t.Errorf("compiled example %s is absent from migration guide", name)
		}
	}
}

func TestLifecycleGuideSnippets(t *testing.T) {
	root, regions := compiledRegions(t, "migration|guide")
	guides := []string{
		"model-declarations", "model-writes", "model-batch-writes", "model-changes",
		"model-hooks", "model-retrieval", "model-global-scopes", "model-timestamps",
		"model-soft-deletes", "model-mutators", "model-accessors", "model-upserts",
		"model-lookup-writes", "model-insert-from-query", "model-source-writes",
		"model-pruning", "query-plans", "audit",
	}
	used := map[string]bool{}
	for _, name := range guides {
		t.Run(name, func(t *testing.T) {
			for region := range checkGuideSnippets(t, filepath.Join(root, "docs/guides", name+".md"), regions) {
				used[region] = true
			}
		})
	}
	_, added := compiledRegions(t, "guide")
	for name := range added {
		if !used[name] {
			t.Errorf("compiled example %s is absent from feature guides", name)
		}
	}
}

func compiledRegions(t *testing.T, family string) (string, map[string]string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate example source")
	}
	dir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(dir, "../../../.."))
	if !filepath.IsAbs(file) {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
			for parent := wd; ; parent = filepath.Dir(parent) {
				if _, err := os.Stat(filepath.Join(parent, "docs/guides")); err == nil {
					root = parent
					break
				}
				if p := filepath.Dir(parent); p == parent {
					break
				}
			}
		}
	}
	sources, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no migration test sources found")
	}
	regions := map[string]string{}
	region := regexp.MustCompile(`(?ms)^// (?:` + family + `):([a-z0-9-]+)\n(.*?)^// (?:` + family + `):end$`)
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range region.FindAllStringSubmatch(string(data), -1) {
			if _, exists := regions[match[1]]; exists {
				t.Fatalf("duplicate source region %s", match[1])
			}
			regions[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return root, regions
}

func checkGuideSnippets(t *testing.T, path string, regions map[string]string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	blocks := regexp.MustCompile("(?s)<!-- orm-example: ([a-z0-9-]+) -->\\n```go\\n(.*?)\\n```").FindAllStringSubmatch(doc, -1)
	if len(blocks) != strings.Count(doc, "```go\n") || len(blocks) == 0 {
		t.Fatal("every guide needs Go blocks naming compiled examples")
	}
	used := map[string]bool{}
	for _, block := range blocks {
		name := block[1]
		if strings.HasSuffix(name, "-before") {
			continue
		} // Retired API examples are documentation only.
		source, exists := regions[name]
		if !exists || used[name] {
			t.Fatalf("missing or duplicate compiled example %s", name)
		}
		if strings.TrimSpace(block[2]) != source {
			t.Errorf("guide example %s differs from compiled source", name)
		}
		used[name] = true
	}
	return used
}

func TestDeclarationsAndReadSQL(t *testing.T) {
	if err := orm.Check(Users.Table, Archives.Table, models.Users.Table, models.Orders.Table); err != nil {
		t.Fatal(err)
	}
	after, err := orm.Compile(models.Orders.Query().Where(orm.NumberGt(models.Orders.TotalCents, 100)))
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "goway/migration/guide_test/1", oracle.Snapshot([]any{after.SQL(), after.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}

	if err := Theme.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := observer(); err != nil {
		t.Fatal(err)
	}
}
