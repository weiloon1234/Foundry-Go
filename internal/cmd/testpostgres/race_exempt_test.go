package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRaceExemptPackagesDoNotStartGoroutines(t *testing.T) {
	root := "../../.."
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "RACE_EXEMPT_PACKAGES := "); ok {
			if paths != nil {
				t.Fatal("race exemptions must have one owner assignment")
			}
			paths = strings.Fields(value)
		}
	}
	if len(paths) == 0 {
		t.Fatal("missing literal RACE_EXEMPT_PACKAGES assignment in Makefile")
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if !strings.HasPrefix(path, "./") || !filepath.IsLocal(path) || strings.ContainsAny(path, "$\\#") {
				t.Fatal("race exemptions must be literal repository-relative package paths")
			}
			if err := checkRaceExemptSource(filepath.Join(root, path)); err != nil {
				t.Fatalf("remove %s from RACE_EXEMPT_PACKAGES: %v", path, err)
			}
		})
	}
}

// Parse every platform's source, without compiling or excluding build tags.
func checkRaceExemptSource(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	files := token.NewFileSet()
	sources := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		sources++
		source, err := parser.ParseFile(files, filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, imported := range source.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if path == "golang.org/x/sync/errgroup" {
				return fmt.Errorf("%s imports errgroup", files.Position(imported.Pos()))
			}
		}
		var goroutine token.Pos
		ast.Inspect(source, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.GoStmt:
				goroutine = node.Pos()
			case *ast.CallExpr:
				if method, ok := ast.Unparen(node.Fun).(*ast.SelectorExpr); ok && method.Sel.Name == "Go" {
					goroutine = node.Pos()
				}
			}
			return goroutine == token.NoPos
		})
		if goroutine != token.NoPos {
			return fmt.Errorf("%s starts a goroutine", files.Position(goroutine))
		}
	}
	if sources == 0 {
		return fmt.Errorf("%s has no non-test Go source", dir)
	}
	return nil
}

func TestRaceExemptionGuardRejectsMissingPackagesAndGoroutines(t *testing.T) {
	root := t.TempDir()
	if err := checkRaceExemptSource(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing package accepted")
	}
	if err := checkRaceExemptSource(root); err == nil {
		t.Fatal("empty package accepted")
	}
	for _, source := range []struct {
		name string
		code string
		fail bool
	}{
		{"plain.go", "package example\nfunc f() {}", false},
		{"plain_test.go", "package example\nfunc test() { go f() }", false},
		{"goroutine.go", "package example\nfunc start() { go f() }", true},
		{"other_platform.go", "//go:build unavailable\n\npackage example\nfunc start() { go f() }", true},
		{"waitgroup.go", "package example\nimport \"sync\"\nfunc start() { var group sync.WaitGroup; group.Go(func() {}) }", true},
		{"other_group.go", "package example\nfunc start() { group.Go(func() {}) }", true},
		{"parenthesized.go", "package example\nfunc start() { (group.Go)(func() {}) }", true},
		{"tagged_group.go", "//go:build unavailable\n\npackage example\nfunc start() { group.Go(func() {}) }", true},
		{"errgroup.go", "package example\nimport \"golang.org/x/sync/errgroup\"", true},
		{"aliased_errgroup.go", "package example\nimport group \"golang.org/x/sync/errgroup\"", true},
		{"raw_errgroup.go", "package example\nimport `golang.org/x/sync/errgroup`", true},
		{"tagged_errgroup.go", "//go:build unavailable\n\npackage example\nimport _ \"golang.org/x/sync/errgroup\"", true},
	} {
		t.Run(source.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "baseline.go"), []byte("package example"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, source.name), []byte(source.code), 0600); err != nil {
				t.Fatal(err)
			}
			if err := checkRaceExemptSource(dir); (err != nil) != source.fail {
				t.Fatalf("%s: %v", source.name, err)
			}
		})
	}
}
