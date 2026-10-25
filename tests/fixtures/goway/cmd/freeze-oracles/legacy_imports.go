package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

// Inspect imports, not quoted package names in generated descriptor metadata.
func legacyModelImporters(root string) (map[string][]string, error) {
	found := map[string][]string{}
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
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, spec := range file.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, family := range []string{"oldmodels", "oldreporting"} {
				prefix := "foundry.test/goway/" + family
				if name == prefix || strings.HasPrefix(name, prefix+"/") {
					found[rel] = append(found[rel], name)
				}
			}
		}
		slices.Sort(found[rel])
		return nil
	})
	return found, err
}

// Every generated-model dependency outside its own package must be part of
// audited oracle capture or the explicit migration-before examples exception.
// These three files supply methods to the oldGolden
// adapter used by the audited comparisons; they have no standalone entry point.
func recordingImporters(fixture string, audit oracle.ComparisonAudit) (map[string][]string, error) {
	root := filepath.Clean(filepath.Join(fixture, "../../.."))
	importers, err := legacyModelImporters(root)
	if err != nil {
		return nil, err
	}
	allowed := map[string][]string{
		"tests/fixtures/goway/golden_old_queries_test.go":   nil,
		"tests/fixtures/goway/golden_old_loading_test.go":   nil,
		"tests/fixtures/goway/golden_old_relations_test.go": nil,
	}
	for file, source := range audit.Files {
		if len(source.Tests) > 0 {
			allowed["tests/fixtures/"+file] = source.Tests
		}
	}
	packages, err := legacyPackages(fixture)
	if err != nil {
		return nil, err
	}
	for file := range importers {
		if migrationBeforeExample(file) {
			continue
		}
		if tests, ok := allowed[file]; ok {
			if err := validateRecordingFile(filepath.Join(root, file), tests); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(file, "tests/fixtures/goway/oldmodels/") || strings.HasPrefix(file, "tests/fixtures/goway/oldreporting/") {
			continue
		}
		footprint := false
		for _, pkg := range packages {
			if strings.HasPrefix(pkg, "./testdata/footprint/") && filepath.ToSlash(filepath.Dir(file)) == "tests/fixtures/goway/"+strings.TrimPrefix(pkg, "./") {
				footprint = true
				break
			}
		}
		if !footprint {
			return nil, fmt.Errorf("generated legacy model import outside oracle recording path: %s", file)
		}
	}
	return importers, nil
}

// A legacy importer is a separate recording file: it cannot also contain an
// ordinary test, benchmark, fuzz target, example or init hook. Supporting
// declarations stay with the recorded entry points; behavior tests live in
// files without generated-model imports. The oldGolden method-only adapters
// have no recorded test entry points of their own.
func validateRecordingFile(path string, recorded []string) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		if name == "init" || strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example") {
			if !slices.Contains(recorded, name) {
				return fmt.Errorf("legacy recording file contains unrecorded entry point %s: %s; move behavior tests to a file without generated-model imports", name, path)
			}
		}
	}
	return nil
}

// migration-before examples: these exact files intentionally compile the guide's
// old-API snippets. In Stage 8c, before blocks become documentation-only while
// after blocks remain compiled. This is not a directory-wide exception.
func migrationBeforeExample(file string) bool {
	switch file {
	case "tests/fixtures/goway/migration/guide_test.go",
		"tests/fixtures/goway/migration/examples_test.go",
		"tests/fixtures/goway/migration/sql_test.go":
		return true
	default:
		return false
	}
}
