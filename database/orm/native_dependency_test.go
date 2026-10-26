package orm_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These exact bridge declarations retire in 8d3, under the program's
// patches/stage8d-decisions.md, decisions 2 and 5. No other declaration in either
// file may depend on the legacy engine.
var nativeLegacyBridges = map[string]string{
	"database/orm/identity_legacy.go":  "LegacyIdentityScope", // retire in 8d3; stage8d-decisions.md
	"database/orm/extension_legacy.go": "Extension",           // retire in 8d3; stage8d-decisions.md
}

func nativeLegacyImportError(path string, source []byte) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		return false, err
	}
	legacy := false
	for _, spec := range file.Imports {
		name, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return false, err
		}
		if name != "github.com/weiloon1234/Foundry-Go/database/query" {
			continue
		}
		bridge, allowed := nativeLegacyBridges[path]
		if !allowed {
			return false, fmt.Errorf("%s imports the legacy engine", path)
		}
		alias := "query"
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "." || alias == "_" {
			return false, fmt.Errorf("%s hides its legacy import", path)
		}
		found := false
		for _, decl := range file.Decls {
			if function, ok := decl.(*ast.FuncDecl); ok && function.Recv == nil && function.Name.Name == bridge {
				found = true
				continue
			}
			if general, ok := decl.(*ast.GenDecl); ok && general.Tok == token.IMPORT {
				continue
			}
			usesLegacy := false
			ast.Inspect(decl, func(node ast.Node) bool {
				if id, ok := node.(*ast.Ident); ok && id.Name == alias {
					usesLegacy = true
				}
				return true
			})
			if usesLegacy {
				return false, fmt.Errorf("%s uses the legacy engine outside %s", path, bridge)
			}
		}
		if !found {
			return false, fmt.Errorf("%s has no allowlisted bridge %s", path, bridge)
		}
		legacy = true
	}
	return legacy, nil
}

func TestNativePackagesDoNotImportLegacyEngine(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := make(map[string]bool)
	// Cover every internal owner, including future owners, as well as all ORM
	// subpackages. Test-only legacy identity assertions are not runtime edges.
	for _, dir := range []string{"database/orm", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			allowed, err := nativeLegacyImportError(filepath.ToSlash(relative), source)
			if err != nil {
				t.Error(err)
			}
			if allowed {
				seen[filepath.ToSlash(relative)] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for path := range nativeLegacyBridges {
		if !seen[path] {
			t.Errorf("remove stale legacy bridge allowlist entry %s", path)
		}
	}
}

func TestNativeDependencyGuardRejectsNewImports(t *testing.T) {
	const source = `package sample
import legacy "github.com/weiloon1234/Foundry-Go/database/query"
func NewDependency() legacy.PageRequest { return legacy.PageRequest{} }
`
	for _, path := range []string{"database/orm/new.go", "database/orm/expr/new.go", "database/orm/rows/new.go", "internal/ormcore/new.go", "internal/querycore/new.go", "internal/future/new.go"} {
		t.Run(path, func(t *testing.T) {
			if _, err := nativeLegacyImportError(path, []byte(source)); err == nil {
				t.Fatal("new legacy dependency accepted")
			}
		})
	}
	for path, bridge := range nativeLegacyBridges {
		allowedSource := strings.ReplaceAll(source, "NewDependency", bridge)
		if allowed, err := nativeLegacyImportError(path, []byte(allowedSource)); err != nil || !allowed {
			t.Fatalf("exact bridge rejected: %v", err)
		}
		for _, bad := range []string{
			source,
			allowedSource + "\nfunc Extra() legacy.PageRequest { return legacy.PageRequest{} }",
			allowedSource + "\ntype Extra = legacy.PageRequest",
			strings.ReplaceAll(allowedSource, "import legacy", "import ."),
			strings.ReplaceAll(allowedSource, "import legacy", "import _"),
		} {
			if _, err := nativeLegacyImportError(path, []byte(bad)); err == nil {
				t.Fatalf("%s widened bridge exception", path)
			}
		}
	}
}
