package oracle

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The only exception is the historical cursor model: one handwritten package,
// imported by one cursor fixture. Recording and migration-before exceptions retire.
// Parsing and unquoting imports also catches escaped paths and build-tagged files.
func legacyModelImporters(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, i := range f.Imports {
			p, err := strconv.Unquote(i.Path.Value)
			if err != nil {
				return err
			}
			for _, family := range []string{"foundry.test/goway/oldmodels", "foundry.test/goway/oldreporting"} {
				if p == family || strings.HasPrefix(p, family+"/") {
					relative, err := filepath.Rel(root, path)
					if err != nil {
						return err
					}
					if filepath.ToSlash(relative) != "tests/fixtures/goway/reads_golden_test.go" || p != "foundry.test/goway/oldmodels" {
						found = append(found, path)
					}
				}
			}
		}
		return nil
	})
	return found, err
}
func TestRetiredModelsHaveNoImporters(t *testing.T) {
	found, err := legacyModelImporters("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("retired generated-model imports: %v", found)
	}
}
func TestRetiredImportGuardHasNoFileExceptions(t *testing.T) {
	for _, path := range []string{"ordinary_test.go", "migration/examples_test.go", "oracle_recording_test.go"} {
		for _, importPath := range []string{`"foundry.test/goway/oldmodels"`, `"foundry.test/goway/old\x6dodels/hooks"`, `"foundry.test/goway/oldreporting"`} {
			dir := t.TempDir()
			p := filepath.Join(dir, path)
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("//go:build legacy\n\npackage sample\nimport _ "+importPath+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			found, err := legacyModelImporters(dir)
			if err != nil || len(found) != 1 {
				t.Fatal(path, found, err)
			}
		}
	}
}

func TestHistoricalCursorImportExceptionIsExact(t *testing.T) {
	for _, tc := range []struct {
		path, importPath string
		allowed          bool
	}{
		{"tests/fixtures/goway/reads_golden_test.go", "foundry.test/goway/oldmodels", true},
		{"tests/fixtures/goway/reads_golden_test.go", "foundry.test/goway/oldmodels/hooks", false},
		{"tests/fixtures/goway/reads_golden_test.go", "foundry.test/goway/oldreporting", false},
		{"tests/fixtures/goway/ordinary_test.go", "foundry.test/goway/oldmodels", false},
		{"elsewhere/reads_golden_test.go", "foundry.test/goway/oldmodels", false},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, tc.path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package fixture\nimport _ "+strconv.Quote(tc.importPath)+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := legacyModelImporters(dir)
		if err != nil || (len(got) == 0) != tc.allowed {
			t.Fatal(tc, got, err)
		}
	}
}

func TestHistoricalCursorFixtureIsHandwrittenOnly(t *testing.T) {
	directory := "../fixtures/goway/oldmodels"
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Name() != "cursor_identity.go" || entry.IsDir() {
			t.Fatalf("historical identity fixture expanded: %s", entry.Name())
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "cursor_identity.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Code generated", "foundry:model", "foundry:projection", "/database/query"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("historical identity fixture depends on retired generation: %s", forbidden)
		}
	}
}
