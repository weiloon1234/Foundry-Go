package migration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"foundry.test/goway/models"
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

// The guide includes exact regions of these compiled examples. Every Go block
// needs a region ID; missing, duplicate and stale snippets fail this test.
func TestGuideSnippets(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration source")
	}
	dir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(dir, "../../../.."))
	sources, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	regions := map[string]string{}
	region := regexp.MustCompile(`(?ms)^// migration:([a-z0-9-]+)\n(.*?)^// migration:end$`)
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
	data, err := os.ReadFile(filepath.Join(root, "docs/guides/orm-migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	blocks := regexp.MustCompile("(?s)<!-- orm-example: ([a-z0-9-]+) -->\\n```go\\n(.*?)\\n```").FindAllStringSubmatch(doc, -1)
	if len(blocks) != strings.Count(doc, "```go\n") {
		t.Fatal("every Go block must name a compiled example")
	}
	used := map[string]bool{}
	for _, block := range blocks {
		name := block[1]
		source, exists := regions[name]
		if !exists || used[name] {
			t.Fatalf("missing or duplicate compiled example %s", name)
		}
		if strings.TrimSpace(block[2]) != source {
			t.Errorf("guide example %s differs from compiled source", name)
		}
		used[name] = true
	}
	for name := range regions {
		if !used[name] {
			t.Errorf("compiled example %s is absent from guide", name)
		}
	}
}

func TestDeclarationsAndReadSQL(t *testing.T) {
	if err := orm.Check(Users.Table, models.Users.Table, models.Orders.Table); err != nil {
		t.Fatal(err)
	}
	before, err := old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(100)).Compile()
	if err != nil {
		t.Fatal(err)
	}
	after, err := orm.Compile(models.Orders.Query().Where(models.Orders.TotalCents.Gt(100)))
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "goway/migration/guide_test/1", oracle.Snapshot([]any{before.SQL(), before.Arguments()}), oracle.Snapshot([]any{after.SQL(), after.Arguments()})) {
		t.Fatal("migration example changed SQL or bound values")
	}
	if err := Theme.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyObserver(); err != nil {
		t.Fatal(err)
	}
	if _, err := observer(); err != nil {
		t.Fatal(err)
	}
}
