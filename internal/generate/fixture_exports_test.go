package generate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Only fixture entry points opt in. Discovery, module selection, recovery and
// publication tests continue to exercise the production Go loader directly.
func generateFixture(ctx context.Context, options Options) (Report, error) {
	options.listExports = fixtureExports.list
	return Generate(ctx, options)
}

func scaffoldFixture(ctx context.Context, options ScaffoldOptions) (string, error) {
	options.listExports = fixtureExports.list
	return Scaffold(ctx, options)
}

var fixtureExports fixtureExportCache

type fixtureExportCache struct {
	mu          sync.Mutex
	environment []string
	packages    map[string]listedPackage
}

// Fixtures have identical module requirements and an immutable local framework.
// Cache dependency metadata, never consumer packages or go/types importers.
// New targets extend the cache; repeated targets reuse their complete closure.
func (cache *fixtureExportCache) list(g *packageGraph, ctx context.Context, arguments []string, receive func(listedPackage)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	files, err := fixtureModuleFiles()
	if err != nil {
		return err
	}
	if !bytes.Equal(g.moduleFiles[filepath.Join(g.scope.root, "go.mod")].data, files.mod) ||
		!bytes.Equal(g.moduleFiles[filepath.Join(g.scope.root, "go.sum")].data, files.sum) || g.scope.vendor || os.Getenv("GOWORK") != "off" {
		return g.goList(ctx, arguments, receive)
	}
	separator := slices.Index(arguments, "--")
	if separator < 0 {
		return fmt.Errorf("fixture export request has no operand separator")
	}
	targets := arguments[separator+1:]
	for _, target := range targets {
		if target == fixtureModulePath || strings.HasPrefix(target, fixtureModulePath+"/") {
			return g.goList(ctx, arguments, receive)
		}
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.packages == nil {
		cache.environment = os.Environ()
		cache.packages = make(map[string]listedPackage)
	} else if !slices.Equal(cache.environment, os.Environ()) {
		return g.goList(ctx, arguments, receive)
	}
	missing := make(map[string]bool)
	for _, target := range targets {
		if cache.packages[target].Export == "" {
			missing[target] = true
		}
	}
	if len(missing) != 0 {
		request := append(slices.Clone(arguments[:separator+1]), sortedNames(missing)...)
		// Publish a complete successful request to the cache, never partial data.
		listed := make(map[string]listedPackage)
		if err := g.goList(ctx, request, func(pkg listedPackage) {
			listed[pkg.ImportPath] = pkg
		}); err != nil {
			return err
		}
		for path, pkg := range listed {
			cache.packages[path] = pkg
		}
	}
	closure := make(map[string]bool)
	for _, target := range targets {
		pkg, exists := cache.packages[target]
		if !exists {
			return fmt.Errorf("fixture dependency %s was not listed by Go", target)
		}
		closure[target] = true
		for _, dependency := range pkg.Deps {
			closure[dependency] = true
		}
	}
	for _, path := range sortedNames(closure) {
		pkg, exists := cache.packages[path]
		if !exists {
			return fmt.Errorf("fixture dependency closure is missing %s", path)
		}
		receive(pkg)
	}
	return nil
}

func TestFixtureExportsMatchLiveGraph(t *testing.T) {
	dir := fixture(t, sampleSource)
	cache := new(fixtureExportCache)
	// Vary the closure to exercise both a cache hit and a newly requested target.
	for _, imports := range [][]string{nil, {framework + "/database/orm"}, {"net/mail"}, nil} {
		live, err := loadGraph(t.Context(), dir, false, nil, imports, nil)
		if err != nil {
			t.Fatal(err)
		}
		cached, err := loadGraph(t.Context(), dir, false, nil, imports, cache.list)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cached.exports, live.exports) ||
			!reflect.DeepEqual(cached.documentationPackages, live.documentationPackages) ||
			!reflect.DeepEqual(cached.framework, live.framework) ||
			!reflect.DeepEqual(cached.persistenceDependents, live.persistenceDependents) {
			t.Fatalf("cached export metadata differs from Go for extra imports %v", imports)
		}
	}
	if os.Getenv("GOWORK") == "off" && len(cache.packages) == 0 {
		t.Fatal("fixture did not exercise the export cache")
	}
	files, err := fixtureModuleFiles()
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "go.mod", string(files.mod)+"\n// changed module inputs\n")
	// Poison only this test's cache: changed inputs must use Go's real exports.
	cache.packages = map[string]listedPackage{framework + "/value": {Export: "not-export-data"}}
	live, err := loadGraph(t.Context(), dir, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := loadGraph(t.Context(), dir, false, nil, nil, cache.list)
	if err != nil || !reflect.DeepEqual(changed.exports, live.exports) || len(cache.packages) != 1 {
		t.Fatalf("changed module inputs reused or populated the fixture cache: %v", err)
	}
}
