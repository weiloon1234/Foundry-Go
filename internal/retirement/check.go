// Package retirement enforces the removed data-layer surface across source modules.
package retirement

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed denied.json
var ledger embed.FS

const framework = "github.com/weiloon1234/Foundry-Go/"
const retiredPath = "database/" + "query"

func declarations() (map[string][]string, error) {
	data, err := ledger.ReadFile("denied.json")
	if err != nil {
		return nil, err
	}
	var result map[string][]string
	err = json.Unmarshal(data, &result)
	return result, err
}

// Check walks source regardless of build selection or module boundaries. Historical
// text is not Go source; no source file or declaration is exempt from these rules.
// The exact public snapshot in database/orm/testdata/public-methods.txt and
// database/orm/method_surface_test.go backstop changes outside this exposure check.
// Direct denied members and local alias chains are checked independently of build
// constraints. Exposure assembled only across mutually exclusive build variants
// remains outside the semantic check; the final public-surface audit covers it.
func Check(root string) error {
	denied, err := declarations()
	if err != nil {
		return err
	}
	var failures []error
	fset := token.NewFileSet()
	packages := make(map[string]*sourcePackage)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		// Reserve the removed path itself, including a symlink to a package.
		if relative == retiredPath || strings.HasSuffix(relative, "/"+retiredPath) {
			failures = append(failures, safeerror.New(fmt.Sprintf("%s: retired directory path", relative)))
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_legacy.go") {
			failures = append(failures, safeerror.New(fmt.Sprintf("%s: retired filename", relative)))
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		imports := make(map[string]string)
		var dotOwners []string
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			alias := pathpkg.Base(imported)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			imports[alias] = strings.TrimPrefix(imported, framework)
			if alias == "." {
				dotOwners = append(dotOwners, imports[alias])
			}
			if imported == framework+retiredPath || strings.HasPrefix(imported, framework+retiredPath+"/") {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s: retired import", relative)))
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				for _, owner := range dotOwners {
					for _, name := range denied[owner] {
						if name == id.Name {
							failures = append(failures, safeerror.New(fmt.Sprintf("%s: dot-import reference to retired %s.%s", relative, owner, name)))
						}
					}
				}
			}
			if selected, ok := n.(*ast.SelectorExpr); ok {
				if qualifier, ok := selected.X.(*ast.Ident); ok {
					for _, name := range denied[imports[qualifier.Name]] {
						if selected.Sel.Name == name {
							failures = append(failures, safeerror.New(fmt.Sprintf("%s: reference to retired %s.%s", relative, qualifier.Name, name)))
						}
					}
				}
			}
			if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Legacy") {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s: retired identifier %s", relative, id.Name)))
			}
			return true
		})
		owner, moduleDir, err := packageLocation(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		// Retain every source file, including tests and build-ignored declarations,
		// for the public exposure check. Different package clauses stay separate.
		key := filepath.Dir(path) + "/" + file.Name.Name
		pkg := packages[key]
		if pkg == nil {
			pkg = &sourcePackage{owner: owner, moduleDir: moduleDir}
			packages[key] = pkg
		}
		pkg.files = append(pkg.files, file)
		return nil
	})
	if err != nil {
		failures = append(failures, err)
	}
	// Syntax violations already fail closed, without compiling a forbidden import.
	if len(failures) == 0 {
		if err := checkMemberDeclarations(fset, packages, denied); err != nil {
			failures = append(failures, err)
		} else {
			failures = append(failures, checkExposures(fset, packages, denied))
		}
	}
	return safeerror.Join("operation failed", failures...)
}

// Resolve the nearest module, including fixture modules and nested source trees.
func packageLocation(root, dir string) (string, string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	current, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	original := current
	for {
		data, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "module" {
					module := strings.Trim(fields[1], "\"")
					relative, err := filepath.Rel(current, original)
					if err != nil {
						return "", "", err
					}
					if relative != "." {
						module += "/" + filepath.ToSlash(relative)
					}
					return module, current, nil
				}
			}
			return "", "", safeerror.New(fmt.Sprintf("%s: missing module path", current))
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		if current == root {
			return "", "", safeerror.New(fmt.Sprintf("%s: source has no module", dir))
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", "", safeerror.New(fmt.Sprintf("%s: source outside root", dir))
		}
		current = parent
	}
}
