package retirement

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
)

type sourcePackage struct {
	owner     string
	moduleDir string
	files     []*ast.File
}

// Resolve imports once per module using its effective build environment. The
// exposure pass checks the collected source itself, independently of build tags.
func exposureImporters(fset *token.FileSet, packages map[string]*sourcePackage) (map[string]types.Importer, error) {
	modules := make(map[string]map[string]bool)
	for _, source := range packages {
		paths := modules[source.moduleDir]
		if paths == nil {
			paths = make(map[string]bool)
			modules[source.moduleDir] = paths
		}
		for _, file := range source.files {
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return nil, err
				}
				// C is a pseudo-package supplied by cgo, not a Go import path.
				if path != "C" {
					paths[path] = true
				}
			}
		}
	}
	result := make(map[string]types.Importer)
	for _, dir := range sortedKeys(modules) {
		imports := sortedKeys(modules[dir])
		if len(imports) == 0 {
			continue
		}
		cmd := exec.Command("go", append([]string{"list", "-export", "-deps", "-json"}, imports...)...)
		cmd.Dir = dir
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("%s: resolve exposure imports: %w", dir, err)
		}
		exports := make(map[string]string)
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var pkg struct{ ImportPath, Export string }
			if err := decoder.Decode(&pkg); err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}
			exports[pkg.ImportPath] = pkg.Export
		}
		result[dir] = importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
			return os.Open(exports[path])
		})
	}
	return result, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
