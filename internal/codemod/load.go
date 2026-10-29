package codemod

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
)

type listedPackage struct {
	Dir, ImportPath, Name, Export, ForTest string
	GoFiles, CompiledGoFiles, Match        []string
	ImportMap                              map[string]string
	DepOnly, Incomplete                    bool
	Error                                  *struct{ Err string }
	DepsErrors                             []struct{ Err string }
}

type sourcePackage struct {
	files []*ast.File
	info  *types.Info
	pkg   *types.Package
}

type limitedOutput struct {
	bytes.Buffer
	remaining int
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.remaining {
		return 0, fmt.Errorf("codemod: package metadata exceeds limit")
	}
	b.remaining -= len(p)
	return b.Buffer.Write(p)
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

// Load exports from the consumer's module graph, never the tool's linked types.
// Like noraw, compiler diagnostics are discarded because they can expose source.
func load(ctx context.Context, dir string, patterns []string, fset *token.FileSet) ([]sourcePackage, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "-") {
			return nil, fmt.Errorf("codemod: package patterns cannot be flags")
		}
	}
	args := append([]string{"list", "-deps", "-export", "-compiled", "-test", "-json"}, patterns...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	out := &limitedOutput{remaining: 64 << 20}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("codemod: package loading failed; resolve current dependency build errors first")
	}
	var selected []listedPackage
	exports := map[string]string{}
	decoder := json.NewDecoder(&out.Buffer)
	for {
		var p listedPackage
		err := decoder.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("codemod: invalid package metadata")
		}
		if p.Incomplete || p.Error != nil || len(p.DepsErrors) > 0 {
			return nil, fmt.Errorf("codemod: incomplete package metadata")
		}
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
		if !p.DepOnly && (len(p.Match) > 0 || p.ForTest != "") {
			selected = append(selected, p)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("codemod: no packages matched")
	}
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		name := exports[path]
		if name == "" {
			return nil, fmt.Errorf("codemod: missing export data")
		}
		return os.Open(name)
	})
	var result []sourcePackage
	for _, p := range selected {
		var files []*ast.File
		for _, name := range p.CompiledGoFiles {
			if !filepath.IsAbs(name) {
				name = filepath.Join(p.Dir, name)
			}
			file, err := parser.ParseFile(fset, name, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return nil, fmt.Errorf("codemod: cannot parse selected source")
			}
			files = append(files, file)
		}
		info := newInfo()
		config := types.Config{Importer: mappedImporter{imp, p.ImportMap}}
		pkg, err := config.Check(p.ImportPath, fset, files, info)
		if err != nil {
			return nil, fmt.Errorf("codemod: cannot type-check selected source against current dependency")
		}
		result = append(result, sourcePackage{files, info, pkg})
	}
	return result, nil
}
func newInfo() *types.Info {
	return &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Scopes: map[ast.Node]*types.Scope{}, Implicits: map[ast.Node]types.Object{}, Instances: map[*ast.Ident]types.Instance{}}
}
