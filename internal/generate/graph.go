package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

type packageGraph struct {
	root        string
	recursive   bool
	packages    map[string]listedPackage
	order       []string
	snapshot    map[string][]string
	moduleFiles map[string]oldFile
	fset        *token.FileSet
	compiled    types.Importer
	checked     map[string]*types.Package
}

// Import shares completed in-memory packages with dependents. Compiled export
// data remains the source for dependencies outside the selected package graph.
func (g *packageGraph) Import(path string) (*types.Package, error) {
	if pkg, ok := g.checked[path]; ok {
		return pkg, nil
	}
	if _, selected := g.packages[path]; selected {
		return nil, fmt.Errorf("selected dependency %s has not completed generation", path)
	}
	return g.compiled.Import(path)
}

func listGraph(ctx context.Context, dir string, recursive bool, extraImports ...string) (*packageGraph, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	snapshot, err := sourceTree(absolute, recursive)
	if err != nil {
		return nil, err
	}
	moduleFiles, err := captureModuleFiles(absolute)
	if err != nil {
		return nil, err
	}
	pattern := "."
	if recursive {
		pattern = "./..."
	}
	arguments := []string{"list", "-mod=readonly", "-e", "-deps", "-export", "-json", pattern, framework + "/database/query", framework + "/database/codec", framework + "/database/lifecycle", framework + "/audit/record", framework + "/http", framework + "/contract", framework + "/value", framework + "/enum", framework + "/i18n/message", "database/sql/driver"}
	arguments = append(arguments, framework+"/config")
	cmd := exec.CommandContext(ctx, "go", append(arguments, extraImports...)...)
	cmd.Dir = absolute
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("load consumer Go packages: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	g := &packageGraph{root: absolute, recursive: recursive, packages: make(map[string]listedPackage), snapshot: snapshot, fset: token.NewFileSet(), checked: make(map[string]*types.Package)}
	g.moduleFiles = moduleFiles
	exports := make(map[string]string)
	decoder := json.NewDecoder(&stdout)
	for {
		var pkg listedPackage
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode go list: %w", err)
		}
		if pkg.Export != "" {
			exports[pkg.ImportPath] = pkg.Export
		}
		if !slices.Contains(pkg.Match, pattern) {
			continue
		}
		if pkg.Name == "" || len(pkg.GoFiles)+len(pkg.CgoFiles) == 0 {
			if pkg.Error != nil {
				return nil, fmt.Errorf("%s: %s", pkg.ImportPath, pkg.Error.Err)
			}
			continue
		}
		pkg.Dir = filepath.Clean(pkg.Dir)
		if _, exists := snapshot[pkg.Dir]; !exists {
			return nil, fmt.Errorf("package directory set changed during discovery; retry")
		}
		g.packages[pkg.ImportPath] = pkg
	}
	if len(g.packages) == 0 {
		return nil, fmt.Errorf("%s: no Go packages found", absolute)
	}
	g.compiled = importer.ForCompiler(g.fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("dependency %s has no compiled export data; include its package with --recursive or resolve its build errors", path)
		}
		return os.Open(file)
	})
	g.order, err = packageOrder(g.packages)
	if err != nil {
		return nil, err
	}
	if err := g.checkSourceTree(); err != nil {
		return nil, err
	}
	return g, nil
}

func packageOrder(packages map[string]listedPackage) ([]string, error) {
	state := make(map[string]int)
	var ordered, stack []string
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 2 {
			return nil
		}
		if state[name] == 1 {
			return fmt.Errorf("Go package import cycle: %s", strings.Join(append(slices.Clone(stack), name), " -> "))
		}
		state[name] = 1
		stack = append(stack, name)
		imports := slices.Clone(packages[name].Imports)
		slices.Sort(imports)
		for _, dependency := range imports {
			if _, selected := packages[dependency]; selected {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		ordered = append(ordered, name)
		return nil
	}
	for _, name := range sortedNames(packages) {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// Follow the directories selected by Go's ./... convention. Nested modules,
// vendor, testdata and hidden/underscore directories are separate scopes.
func sourceTree(root string, recursive bool) (map[string][]string, error) {
	result := make(map[string][]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			name := entry.Name()
			if !recursive || name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Lstat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		names, err := goSourceNames(path)
		if err != nil {
			return err
		}
		if len(names) > 0 || path == root {
			result[path] = names
		}
		return nil
	})
	return result, err
}
func (g *packageGraph) checkSourceTree() error {
	current, err := sourceTree(g.root, g.recursive)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(g.snapshot, current) {
		return fmt.Errorf("Go source package/file set changed during generation; retry")
	}
	for name, before := range g.moduleFiles {
		after, err := readFile(name)
		if err != nil {
			return err
		}
		if state(after) != state(before) {
			return fmt.Errorf("module/workspace file %s changed during generation; retry", name)
		}
	}
	return nil
}

// Capture the nearest module and workspace inputs, including absent sum files.
// Generation honors the caller's GOWORK selection without modifying module files.
func captureModuleFiles(root string) (map[string]oldFile, error) {
	files := make(map[string]oldFile)
	capture := func(path string) (bool, error) {
		file, err := readFile(path)
		if err != nil {
			return false, err
		}
		files[path] = file
		return file.exists, nil
	}
	moduleFound := false
	workFound := false
	if work := os.Getenv("GOWORK"); work != "" && work != "auto" {
		workFound = true
		if work != "off" {
			path, err := filepath.Abs(work)
			if err != nil {
				return nil, err
			}
			if _, err := capture(path); err != nil {
				return nil, err
			}
			if _, err := capture(path + ".sum"); err != nil {
				return nil, err
			}
		}
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		if !moduleFound {
			found, err := capture(filepath.Join(dir, "go.mod"))
			if err != nil {
				return nil, err
			}
			if found {
				moduleFound = true
				if _, err := capture(filepath.Join(dir, "go.sum")); err != nil {
					return nil, err
				}
			}
		}
		if !workFound {
			found, err := capture(filepath.Join(dir, "go.work"))
			if err != nil {
				return nil, err
			}
			if found {
				workFound = true
				if _, err := capture(filepath.Join(dir, "go.work.sum")); err != nil {
					return nil, err
				}
			}
		}
		if (moduleFound && workFound) || filepath.Dir(dir) == dir {
			break
		}
	}
	return files, nil
}

type packageWrite struct {
	input *packageInput
	plan  writePlan
}

func (g *packageGraph) prepare(ctx context.Context) ([]packageWrite, error) {
	var writes []packageWrite
	tables := make(map[string]string)
	messageKeys := make(map[string]string)
	for _, path := range g.order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := g.loadPackage(path)
		if err != nil {
			return nil, err
		}
		metadata, err := discover(p)
		if err != nil {
			return nil, err
		}
		for _, m := range metadata.models {
			owner := path + "." + m.name
			if previous, exists := tables[m.table]; exists {
				return nil, fmt.Errorf("duplicate model table %s in %s and %s", m.table, previous, owner)
			}
			tables[m.table] = owner
		}
		for _, declaration := range metadata.dtos {
			if declaration.message == nil {
				continue
			}
			owner := path + "." + declaration.name
			if previous, exists := messageKeys[declaration.message.key]; exists {
				return nil, fmt.Errorf("duplicate message key in %s and %s", previous, owner)
			}
			messageKeys[declaration.message.key] = owner
		}
		notes, err := planFieldDocumentation(p, &metadata)
		if err != nil {
			return nil, err
		}
		outputs, err := emit(p, metadata)
		if err != nil {
			return nil, err
		}
		if err := p.check(outputs); err != nil {
			return nil, err
		}
		g.checked[path] = p.checked
		plan, err := planWrite(p, outputs)
		if err != nil {
			return nil, err
		}
		if err := addFieldDocumentation(p, &plan, notes); err != nil {
			return nil, err
		}
		writes = append(writes, packageWrite{p, plan})
	}
	return writes, g.checkSourceTree()
}
