package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type listedModule struct {
	Path, Version, Dir string
	Main               bool
	Replace            *listedModule
}

type packageGraph struct {
	root        string
	scope       moduleScope
	recursive   bool
	packages    map[string]listedPackage
	order       []string
	snapshot    map[string][]string
	moduleFiles map[string]oldFile
	fset        *token.FileSet
	// framework is the Foundry-Go module selected by the consumer's build list.
	framework *listedModule
	// fieldDocumentation opts into managed notes in handwritten model files.
	fieldDocumentation bool
	// overlay adds proposed handwritten files, by absolute path, to their
	// package for a validation-only generation that publishes nothing.
	overlay map[string][]byte
	// exports locates compiled export data by import path.
	exports map[string]string

	compiledMu sync.Mutex
	compiled   types.Importer
	checkedMu  sync.RWMutex
	checked    map[string]*types.Package
}

// Import shares completed in-memory packages with dependents. Compiled export
// data remains the source for dependencies outside the selected package graph.
func (g *packageGraph) Import(path string) (*types.Package, error) {
	g.checkedMu.RLock()
	pkg, ok := g.checked[path]
	g.checkedMu.RUnlock()
	if ok {
		return pkg, nil
	}
	if _, selected := g.packages[path]; selected {
		return nil, fmt.Errorf("selected dependency %s has not completed generation", path)
	}
	// The export-data importer caches packages in an unsynchronized map.
	g.compiledMu.Lock()
	defer g.compiledMu.Unlock()
	return g.compiled.Import(path)
}

func listGraph(ctx context.Context, dir string, recursive bool, extraImports ...string) (*packageGraph, error) {
	return loadGraph(ctx, dir, recursive, nil, extraImports)
}

// loadGraph lists the selected packages without compiling them, then requests
// compiled export data only for dependencies outside the selection. A tree
// captured by recovery is reused so one run walks the source tree once.
func loadGraph(ctx context.Context, dir string, recursive bool, tree *generationTree, extraImports []string) (*packageGraph, error) {
	if tree == nil {
		var err error
		tree, err = walkGenerationTree(ctx, dir, recursive)
		if err != nil {
			return nil, err
		}
	}
	moduleFiles, err := captureModuleFiles(tree.root)
	if err != nil {
		return nil, err
	}
	g := &packageGraph{root: tree.root, scope: tree.scope, recursive: recursive, packages: make(map[string]listedPackage), snapshot: tree.dirs, moduleFiles: moduleFiles, fset: token.NewFileSet(), checked: make(map[string]*types.Package)}
	pattern := "."
	if recursive {
		pattern = "./..."
	}
	arguments := append([]string{"list"}, g.scope.listFlags()...)
	arguments = append(arguments, "-e", "-json=Dir,ImportPath,Name,GoFiles,CgoFiles,IgnoredGoFiles,Imports,Match,Error", "--", pattern)
	var listed []listedPackage
	if err := g.goList(ctx, arguments, func(pkg listedPackage) { listed = append(listed, pkg) }); err != nil {
		return nil, err
	}
	for _, pkg := range listed {
		if !slices.Contains(pkg.Match, pattern) {
			continue
		}
		pkg.Dir = filepath.Clean(pkg.Dir)
		if excludedDirectory(g.root, pkg.Dir) {
			continue
		}
		include, err := g.selectPackage(pkg)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}
		if _, exists := g.snapshot[pkg.Dir]; !exists {
			return nil, fmt.Errorf("package directory set changed during discovery; retry")
		}
		g.packages[pkg.ImportPath] = pkg
	}
	if len(g.packages) == 0 && len(listed) == 0 {
		return nil, fmt.Errorf("%s: no Go packages found", g.scope.display(g.root))
	}
	external := make(map[string]bool)
	for _, path := range sortedNames(g.packages) {
		pkg := g.packages[path]
		if err := g.checkSourceImportOptions(ctx, pkg); err != nil {
			return nil, err
		}
		for _, dependency := range pkg.Imports {
			if _, selected := g.packages[dependency]; !selected && dependency != "C" && dependency != "unsafe" {
				external[dependency] = true
			}
		}
	}
	targets := sortedNames(external)
	targets = append(targets, framework+"/database/query", framework+"/database/codec", framework+"/database/lifecycle", framework+"/audit/record", framework+"/http", framework+"/contract", framework+"/value", framework+"/enum", framework+"/i18n/message", framework+"/config", "database/sql/driver", "sync")
	// Generated extension slot declarations import the binding package. Slot
	// field types require one of its extension packages, so other consumers do
	// not compile attachment and imaging export data for generation.
	if external[framework+"/translations"] || external[framework+"/attachments"] || external[framework+"/metadata"] {
		targets = append(targets, framework+"/extensions/slots")
	}
	targets = append(targets, extraImports...)
	// Check metadata and scaffold imports as well as the selected source files.
	for _, target := range targets {
		if err := checkImportOption(target); err != nil {
			return nil, fmt.Errorf("%s: %w", g.scope.display(g.root), err)
		}
	}
	arguments = append([]string{"list"}, g.scope.listFlags()...)
	arguments = append(arguments, "-e", "-deps", "-export", "-json=ImportPath,Export,Module", "--")
	arguments = append(arguments, targets...)
	exports := make(map[string]string)
	if err := g.goList(ctx, arguments, func(pkg listedPackage) {
		if pkg.Export != "" {
			exports[pkg.ImportPath] = pkg.Export
		}
		if pkg.ImportPath == framework+"/value" && pkg.Module != nil {
			module := *pkg.Module
			g.framework = &module
		}
	}); err != nil {
		return nil, err
	}
	// Slot field types can reach a package only through an alias declared
	// outside the generated set. The export closure then holds an extension
	// package without the binding package generated declarations import.
	slotsPath := framework + "/extensions/slots"
	if _, listed := exports[slotsPath]; !listed && (exports[framework+"/translations"] != "" || exports[framework+"/attachments"] != "" || exports[framework+"/metadata"] != "") {
		arguments = append(append([]string{"list"}, g.scope.listFlags()...), "-e", "-deps", "-export", "-json=ImportPath,Export,Module", "--", slotsPath)
		if err := g.goList(ctx, arguments, func(pkg listedPackage) {
			if pkg.Export != "" {
				exports[pkg.ImportPath] = pkg.Export
			}
		}); err != nil {
			return nil, err
		}
	}
	g.exports = exports
	g.compiled = g.exportImporter()
	g.order, err = packageOrder(g.packages)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// checkSourceImportOptions rejects flag-shaped imports even when tolerant Go
// metadata omits their paths. Inspect imports in selected generated files too,
// but leave ordinary parse/type errors to the source and generated overlay
// checks: stale generated declarations must remain replaceable.
func (g *packageGraph) checkSourceImportOptions(ctx context.Context, pkg listedPackage) error {
	names := slices.Clone(pkg.GoFiles)
	slices.Sort(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := g.scope.display(filepath.Join(pkg.Dir, name))
		data, err := os.ReadFile(filepath.Join(pkg.Dir, name))
		if err != nil {
			return err
		}
		syntax, _ := parser.ParseFile(g.fset, path, data, parser.ImportsOnly)
		if syntax == nil {
			continue
		}
		for _, imported := range syntax.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				continue
			}
			if err := checkImportOption(path); err != nil {
				return fmt.Errorf("%s: %w", g.fset.Position(imported.Path.Pos()), err)
			}
		}
	}
	return nil
}

func checkImportOption(path string) error {
	if strings.HasPrefix(path, "-") {
		return fmt.Errorf("invalid Go import path %q: leading dash", path)
	}
	return nil
}

func (g *packageGraph) exportImporter() types.Importer {
	return importer.ForCompiler(g.fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := g.exports[path]
		if !ok {
			return nil, fmt.Errorf("dependency %s has no compiled export data; include its package with --recursive or resolve its build errors", path)
		}
		return os.Open(file)
	})
}

// withOverlay returns an independent graph over g's package listing and
// export data that adds proposed handwritten files, by absolute path, for a
// validation-only generation. It has its own importer and type-check results.
func (g *packageGraph) withOverlay(files map[string][]byte) *packageGraph {
	clone := &packageGraph{root: g.root, scope: g.scope, recursive: g.recursive, packages: g.packages, order: g.order, snapshot: g.snapshot, moduleFiles: g.moduleFiles, fset: g.fset, framework: g.framework, fieldDocumentation: g.fieldDocumentation, overlay: files, exports: g.exports, checked: make(map[string]*types.Package)}
	clone.compiled = clone.exportImporter()
	return clone
}

func (g *packageGraph) goList(ctx context.Context, arguments []string, receive func(listedPackage)) error {
	cmd := exec.CommandContext(ctx, "go", arguments...)
	cmd.Dir = g.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("load consumer Go packages: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	decoder := json.NewDecoder(&stdout)
	for {
		var pkg listedPackage
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode go list: %w", err)
		}
		receive(pkg)
	}
}

// selectPackage keeps generation independent of platform-only and cgo packages
// that contain no Foundry declarations. Declarations in either place fail with
// their source path, because generated output must build on every platform.
func (g *packageGraph) selectPackage(pkg listedPackage) (bool, error) {
	var constrained []string
	for _, name := range pkg.IgnoredGoFiles {
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			constrained = append(constrained, name)
		}
	}
	file, err := declarationFile(pkg.Dir, constrained)
	if err != nil {
		return false, err
	}
	if file != "" {
		return false, fmt.Errorf("%s: Foundry declarations are excluded by this platform's build constraints; move them to a file without build constraints so their generated output is not treated as obsolete", g.scope.display(filepath.Join(pkg.Dir, file)))
	}
	if pkg.Name == "" || len(pkg.GoFiles)+len(pkg.CgoFiles) == 0 {
		if len(constrained) != 0 {
			return false, nil
		}
		if pkg.Error != nil {
			return false, fmt.Errorf("%s: %s", g.scope.display(pkg.Dir), pkg.Error.Err)
		}
		return false, nil
	}
	if len(pkg.CgoFiles) == 0 {
		return true, nil
	}
	file, err = declarationFile(pkg.Dir, append(slices.Clone(pkg.GoFiles), pkg.CgoFiles...))
	if err != nil {
		return false, err
	}
	if file != "" {
		return false, fmt.Errorf("%s: Foundry declarations must live in a Go package without cgo; move them to a separate package", g.scope.display(filepath.Join(pkg.Dir, file)))
	}
	owned, err := readFile(filepath.Join(pkg.Dir, manifestName))
	if err != nil {
		return false, err
	}
	if owned.exists {
		return false, fmt.Errorf("%s: cgo package retains Foundry generated output without declarations; review and remove its owned output before generation", g.scope.display(pkg.Dir))
	}
	return false, nil
}

// declarationFile reports the first file with a Foundry type directive. Only
// candidate files mentioning the directive prefix are parsed.
func declarationFile(dir string, names []string) (string, error) {
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	for _, name := range sorted {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		if bytes.HasPrefix(data, []byte(generatedHeader+"\n")) || !mayDeclare(data) {
			continue
		}
		declared, err := hasDeclarationDirective(name, data)
		if err != nil {
			return "", err
		}
		if declared {
			return name, nil
		}
	}
	return "", nil
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

// generationTree is one source walk shared by recovery and package discovery.
type generationTree struct {
	root  string
	scope moduleScope
	dirs  map[string][]string
}

func walkGenerationTree(ctx context.Context, dir string, recursive bool) (*generationTree, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	scope, err := loadModuleScope(ctx, absolute)
	if err != nil {
		return nil, err
	}
	dirs, err := sourceTree(absolute, recursive, scope)
	if err != nil {
		return nil, err
	}
	return &generationTree{root: absolute, scope: scope, dirs: dirs}, nil
}

// Follow the directories selected by Go's ./... convention. Nested modules,
// vendor, testdata, hidden/underscore directories and go.mod ignore entries are
// separate scopes; node_modules is frontend tooling, never generation input.
func sourceTree(root string, recursive bool, scope moduleScope) (map[string][]string, error) {
	result := make(map[string][]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			if !recursive || skippedDirectory(entry.Name()) || scope.ignored(path) {
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

func skippedDirectory(name string) bool {
	return name == "vendor" || name == "testdata" || name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// excludedDirectory applies generation-only exclusions that Go's own ./...
// pattern does not know about, such as frontend node_modules trees.
func excludedDirectory(root, dir string) bool {
	relative, err := filepath.Rel(root, dir)
	if err != nil || relative == "." || outsideRoot(relative) {
		return false
	}
	for _, name := range strings.Split(filepath.ToSlash(relative), "/") {
		if name == "node_modules" {
			return true
		}
	}
	return false
}

func (g *packageGraph) checkSourceTree() error {
	current, err := sourceTree(g.root, g.recursive, g.scope)
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

// preparedPackage is one package's analysis. done closes after err/input are
// final, so dependents can wait without sharing partially checked state.
type preparedPackage struct {
	input    *packageInput
	metadata metadata
	plan     writePlan
	err      error
	done     chan struct{}
}

var errDependencyFailed = errors.New("generation dependency failed")

// prepare analyzes independent packages concurrently with bounded workers. A
// package starts only after its selected dependencies completed. Errors and
// cross-package checks are reported in dependency order, so the result is the
// same as a sequential run.
func (g *packageGraph) prepare(ctx context.Context) ([]packageWrite, error) {
	index := make(map[string]int, len(g.order))
	for i, path := range g.order {
		index[path] = i
	}
	results := make([]preparedPackage, len(g.order))
	for i := range results {
		results[i].done = make(chan struct{})
	}
	slots := make(chan struct{}, generationWorkers())
	var failureMu sync.Mutex
	firstFailure := len(g.order)
	var group sync.WaitGroup
	for i, path := range g.order {
		group.Go(func() {
			result := &results[i]
			defer close(result.done)
			for _, dependency := range g.packages[path].Imports {
				if position, selected := index[dependency]; selected {
					<-results[position].done
					if results[position].err != nil {
						result.err = errDependencyFailed
						return
					}
				}
			}
			failureMu.Lock()
			skip := i > firstFailure
			failureMu.Unlock()
			if skip {
				// A lower-ordered failure is already the reported result.
				result.err = errDependencyFailed
				return
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				result.err = ctx.Err()
				return
			}
			defer func() { <-slots }()
			result.err = g.preparePackage(ctx, path, result)
			if result.err != nil {
				failureMu.Lock()
				firstFailure = min(firstFailure, i)
				failureMu.Unlock()
			}
		})
	}
	group.Wait()
	var writes []packageWrite
	tables := make(map[string]string)
	messageKeys := make(map[string]string)
	for i, path := range g.order {
		result := results[i]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result.err != nil {
			if errors.Is(result.err, errDependencyFailed) {
				continue
			}
			return nil, result.err
		}
		for _, m := range result.metadata.models {
			owner := path + "." + m.name
			if previous, exists := tables[m.table]; exists {
				return nil, fmt.Errorf("%s: duplicate model table %s in %s and %s", m.position, m.table, previous, owner)
			}
			tables[m.table] = owner
		}
		for _, declaration := range result.metadata.dtos {
			if declaration.message == nil {
				continue
			}
			owner := path + "." + declaration.name
			if previous, exists := messageKeys[declaration.message.key]; exists {
				return nil, fmt.Errorf("%s: duplicate message key in %s and %s", declaration.position, previous, owner)
			}
			messageKeys[declaration.message.key] = owner
		}
		writes = append(writes, packageWrite{result.input, result.plan})
	}
	if len(writes) != len(g.order) {
		return nil, fmt.Errorf("generation dependency failed without a reported cause")
	}
	return writes, nil
}

func (g *packageGraph) preparePackage(ctx context.Context, path string, result *preparedPackage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := g.loadPackage(path)
	if err != nil {
		return err
	}
	metadata, err := discover(p)
	if err != nil {
		return err
	}
	var notes map[string][]byte
	if g.fieldDocumentation {
		notes, err = planFieldDocumentation(p, &metadata)
		if err != nil {
			return err
		}
	}
	outputs, origins, err := emit(p, metadata)
	if err != nil {
		return err
	}
	if err := p.check(outputs, origins); err != nil {
		return err
	}
	g.checkedMu.Lock()
	g.checked[path] = p.checked
	g.checkedMu.Unlock()
	plan, err := planWrite(p, outputs)
	if err != nil {
		return err
	}
	if err := addFieldDocumentation(p, &plan, notes); err != nil {
		return err
	}
	result.input, result.metadata, result.plan = p, metadata, plan
	return nil
}

// generationWorkers bounds concurrent package analysis and retained syntax.
func generationWorkers() int { return max(1, min(runtime.GOMAXPROCS(0), 8)) }
