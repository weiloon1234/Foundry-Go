package noraw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

type listedPackage struct {
	Dir, ImportPath, Name, Export string
	GoFiles, CompiledGoFiles      []string
	Match                         []string
	ImportMap                     map[string]string
	DepOnly, Incomplete           bool
	Error                         *struct{ Err string }
	DepsErrors                    []struct{ Err string }
}
type packageGraph struct {
	fset     *token.FileSet
	importer types.Importer
	exports  map[string]string
	selected []listedPackage
}

// Bound tool output and discard compiler diagnostics: they can contain literals
// from application source. A load failure never becomes a clean lint result.
type boundedOutput struct {
	data      bytes.Buffer
	remaining int
	cancel    context.CancelFunc
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.remaining {
		b.cancel()
		return 0, fmt.Errorf("output limit exceeded")
	}
	b.remaining -= len(p)
	return b.data.Write(p)
}
func load(ctx context.Context, dir string, patterns []string) (*packageGraph, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "-") {
			return nil, fmt.Errorf("no-raw: package patterns cannot be flags")
		}
	}
	args := append([]string{"list", "-deps", "-export", "-compiled", "-test", "-json"}, patterns...)
	args = append(args, rawPackages...)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	out := &boundedOutput{remaining: 64 << 20, cancel: cancel}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("no-raw: package loading failed (resolve Go build errors first)")
	}
	g := &packageGraph{fset: token.NewFileSet(), exports: map[string]string{}}
	decoder := json.NewDecoder(&out.data)
	for {
		var p listedPackage
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("no-raw: invalid package metadata")
		}
		if p.Error != nil || len(p.DepsErrors) > 0 || p.Incomplete {
			return nil, fmt.Errorf("no-raw: package loading failed")
		}
		if p.Export != "" {
			g.exports[p.ImportPath] = p.Export
		}
		if !p.DepOnly && !rawMetadataOnly(p, patterns) {
			generated, err := generatedTestMain(p)
			if err != nil {
				return nil, err
			}
			if !generated {
				g.selected = append(g.selected, p)
			}
		}
	}
	if len(g.selected) == 0 {
		return nil, fmt.Errorf("no-raw: no Go packages matched")
	}
	g.importer = importer.ForCompiler(g.fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := g.exports[path]
		if !ok {
			return nil, fmt.Errorf("missing package export")
		}
		return os.Open(file)
	})
	return g, nil
}

type mappedImporter struct {
	base  types.Importer
	paths map[string]string
}

func (i mappedImporter) Import(path string) (*types.Package, error) {
	if mapped := i.paths[path]; mapped != "" {
		path = mapped
	}
	return i.base.Import(path)
}
func (g *packageGraph) check(p listedPackage) ([]*ast.File, *types.Info, error) {
	files := make([]*ast.File, 0, len(p.CompiledGoFiles))
	for _, name := range p.CompiledGoFiles {
		if !filepath.IsAbs(name) {
			name = filepath.Join(p.Dir, name)
		}
		file, err := parser.ParseFile(g.fset, name, nil, 0)
		if err != nil {
			return nil, nil, fmt.Errorf("no-raw: cannot parse selected Go source")
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no-raw: package has no compiled Go source")
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Instances: map[*ast.Ident]types.Instance{}}
	config := types.Config{Importer: mappedImporter{g.importer, p.ImportMap}}
	if _, err := config.Check(p.ImportPath, g.fset, files, info); err != nil {
		return nil, nil, fmt.Errorf("no-raw: cannot type-check selected Go source")
	}
	return files, info, nil
}

// go list supplies the generated test binary's only source as an absolute cache
// file, outside the package directory. Verify its generator marker, not its name:
// real consumer packages (including main packages) may legitimately end in .test.
func generatedTestMain(p listedPackage) (bool, error) {
	if p.Name != "main" || len(p.Match) != 0 || len(p.GoFiles) != 1 || !filepath.IsAbs(p.GoFiles[0]) {
		return false, nil
	}
	rel, err := filepath.Rel(p.Dir, p.GoFiles[0])
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return false, nil
	}
	f, err := os.Open(p.GoFiles[0])
	if err != nil {
		return false, fmt.Errorf("no-raw: cannot inspect generated test metadata")
	}
	defer f.Close()
	prefix, err := io.ReadAll(io.LimitReader(f, 128))
	if err != nil {
		return false, fmt.Errorf("no-raw: cannot inspect generated test metadata")
	}
	return bytes.HasPrefix(bytes.TrimSpace(prefix), []byte("// Code generated by 'go test'. DO NOT EDIT.")), nil
}

// Extra framework packages supply signature metadata, not additional user pattern
// matches. A missing user pattern must still fail even though metadata loaded.
func rawMetadataOnly(p listedPackage, patterns []string) bool {
	if p.ImportPath != frameworkinfo.ModulePath && !strings.HasPrefix(p.ImportPath, frameworkinfo.ModulePath+"/") {
		return false
	}
	for _, match := range p.Match {
		for _, pattern := range patterns {
			if match == pattern {
				return false
			}
		}
	}
	return true
}
