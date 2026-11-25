// Package retirement enforces retired APIs in source and active documentation.
package retirement

import (
	"bytes"
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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed denied.json document_rules.json exported-surface.txt
var ledger embed.FS

const framework = "github.com/weiloon1234/Foundry-Go/"
const retiredPath = "database/" + "query"

// A retired name records either the API that replaces it or, explicitly, that
// nothing does; an entry with neither is a ledger error.
type retiredDeclaration struct {
	Name          string `json:"name"`
	Replacement   string `json:"replacement,omitempty"`
	NoReplacement bool   `json:"noReplacement,omitempty"`
}

var retiredDeclarations = sync.OnceValues(func() (map[string][]retiredDeclaration, error) {
	data, err := ledger.ReadFile("denied.json")
	if err != nil {
		return nil, err
	}
	return parseLedger(data, LedgerFile)
})

func parseLedger(data []byte, source string) (map[string][]retiredDeclaration, error) {
	var result map[string][]retiredDeclaration
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, safeerror.New(fmt.Sprintf("%s:0: %s; the ledger is a JSON object of package paths to arrays of {name, replacement | noReplacement} entries", source, err))
	}
	var failures []error
	for owner, members := range result {
		for _, member := range members {
			if (member.Replacement == "") == !member.NoReplacement {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s:0: %s.%s must have exactly one of replacement and noReplacement: true; name its replacement, or set noReplacement when nothing replaces it", source, owner, member.Name)))
			}
		}
	}
	return result, safeerror.Join("operation failed", failures...)
}

// LedgerNames is the embedded ledger as the release file records its names.
func LedgerNames() ([]byte, error) {
	entries, err := retiredDeclarations()
	if err != nil {
		return nil, err
	}
	return RetiredNames(entries), nil
}

// RetiredNames is the ledger's names as the release file records them: one
// "package<TAB>name" line each, sorted, so the file does not depend on the
// entry schema of its time.
func RetiredNames(entries map[string][]retiredDeclaration) []byte {
	var lines []string
	for owner, members := range entries {
		for _, member := range members {
			lines = append(lines, owner+"\t"+member.Name)
		}
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n") + "\n")
}

// LostEntries names each name of the previous release's ledger that the
// current ledger no longer holds: a retired name stays retired.
func LostEntries(previous []byte, current map[string][]retiredDeclaration) []string {
	have := ledgerNames(current)
	var lost []string
	for _, line := range strings.Split(string(previous), "\n") {
		owner, name, ok := strings.Cut(line, "\t")
		if ok && !have[owner][name] {
			lost = append(lost, fmt.Sprintf("%s:0: %s.%s was retired in the previous release and is missing from the ledger; restore the entry", LedgerFile, owner, name))
		}
	}
	sort.Strings(lost)
	return lost
}

func declarations() (map[string][]string, error) {
	entries, err := retiredDeclarations()
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string, len(entries))
	for owner, members := range entries {
		for _, member := range members {
			result[owner] = append(result[owner], member.Name)
		}
	}
	return result, nil
}

// Check walks source regardless of build selection or module boundaries. Historical
// text and the root local cache are excluded from these rules.
// The exact public snapshot in database/orm/testdata/public-methods.txt and
// database/orm/method_surface_test.go backstop changes outside this exposure check.
// Direct denied members and local alias chains are checked independently of build
// constraints. Exposure assembled only across mutually exclusive build variants
// remains outside the semantic check; the final public-surface audit covers it.
func Check(root string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	denied, err := declarations()
	if err != nil {
		return err
	}
	var failures []error
	fset := token.NewFileSet()
	packages := make(map[string]*sourcePackage)
	locations := make(map[string]sourceLocation)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		// Ignore the repository's local cache, Git metadata, and non-Go tool directories.
		if d.IsDir() {
			if relative == ".cache" || relative == ".git" || strings.HasSuffix(relative, "/.git") || relative == ".github" || relative == ".agents" {
				return filepath.SkipDir
			}
		}
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
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
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
			if owner, retired := retiredPackage(denied, strings.TrimPrefix(imported, framework)); retired {
				failures = append(failures, safeerror.New(fmt.Sprintf("%s: retired import of %s", relative, owner)))
			}
		}
		// A compile-fail case is a wrong guess that must keep failing to compile;
		// its runner asserts the compiler's diagnostic, so naming a retired name
		// there is not a use of it. Imports and declarations stay checked.
		compileFail := strings.Contains("/"+relative, "/testdata/compilefail/")
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && !compileFail {
				for _, owner := range dotOwners {
					for _, name := range denied[owner] {
						if name == id.Name {
							failures = append(failures, safeerror.New(fmt.Sprintf("%s: dot-import reference to retired %s.%s", relative, owner, name)))
						}
					}
				}
			}
			if selected, ok := n.(*ast.SelectorExpr); ok && !compileFail {
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
		location, err := packageLocation(absoluteRoot, filepath.Join(absoluteRoot, filepath.Dir(relative)), locations)
		if err != nil {
			return err
		}
		// Retain every source file, including tests and build-ignored declarations,
		// for the public exposure check. Different package clauses stay separate.
		key := filepath.Dir(path) + "/" + file.Name.Name
		pkg := packages[key]
		if pkg == nil {
			pkg = &sourcePackage{owner: location.owner, moduleDir: location.moduleDir}
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

// retiredPackage reports whether imported, a framework-relative path, is a
// package the ledger retires or lies below one.
func retiredPackage(denied map[string][]string, imported string) (string, bool) {
	for owner, names := range denied {
		if (imported == owner || strings.HasPrefix(imported, owner+"/")) && slices.Contains(names, PackageEntry) {
			return owner, true
		}
	}
	return "", false
}

type sourceLocation struct {
	owner, moduleDir string
}

// Resolve each directory once within Check, including nested module boundaries.
// Fixture mutations in a later Check always get a fresh location cache.
func packageLocation(root, dir string, locations map[string]sourceLocation) (sourceLocation, error) {
	if location, ok := locations[dir]; ok {
		return location, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" {
				location := sourceLocation{owner: strings.Trim(fields[1], "\""), moduleDir: dir}
				locations[dir] = location
				return location, nil
			}
		}
		return sourceLocation{}, safeerror.New(fmt.Sprintf("%s: missing module path", dir))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sourceLocation{}, err
	}
	if dir == root {
		return sourceLocation{}, safeerror.New(fmt.Sprintf("%s: source has no module", dir))
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return sourceLocation{}, safeerror.New(fmt.Sprintf("%s: source outside root", dir))
	}
	location, err := packageLocation(root, parent, locations)
	if err != nil {
		return sourceLocation{}, err
	}
	location.owner += "/" + filepath.Base(dir)
	locations[dir] = location
	return location, nil
}
