package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const maxMetadataBytes = 64 << 20

type listedPackage struct {
	Dir, ImportPath, Name, Export, ForTest string
	GoFiles, CompiledGoFiles, Match        []string
	ImportMap                              map[string]string
	DepOnly, Incomplete                    bool
	Error                                  *struct{ Err string }
	DepsErrors                             []struct{ Err string }
}

type sourcePackage struct {
	path  string
	files []*ast.File
	info  *types.Info
}

type limitedBuffer struct {
	bytes.Buffer
	remaining int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.remaining {
		return 0, errors.New("package metadata exceeds its limit")
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

// load type-checks the module in dir against the dependency versions it pins,
// never against the types this tool was built with. The old constructor
// signatures are therefore the ones the code is rewritten from.
func load(ctx context.Context, dir string, patterns, tags []string, fset *token.FileSet) ([]sourcePackage, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "-") {
			return nil, errors.New("package patterns cannot be flags")
		}
	}
	args := []string{"list", "-deps", "-export", "-compiled", "-test", "-json"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	cmd := exec.CommandContext(ctx, "go", append(args, patterns...)...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	out := &limitedBuffer{remaining: maxMetadataBytes}
	var stderr bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, errors.New("go list failed; the module must build against its current dependency before migrating:\n" + strings.TrimSpace(stderr.String()))
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
			return nil, errors.New("invalid package metadata")
		}
		if p.Incomplete || p.Error != nil || len(p.DepsErrors) > 0 {
			return nil, errors.New("incomplete package metadata for " + p.ImportPath + "; the module must build against its current dependency before migrating")
		}
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
		if !p.DepOnly && (len(p.Match) > 0 || p.ForTest != "") {
			selected = append(selected, p)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("no packages matched")
	}
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		name := exports[path]
		if name == "" {
			return nil, errors.New("missing export data for " + path)
		}
		return os.Open(name)
	})
	var result []sourcePackage
	for _, p := range selected {
		if strings.HasSuffix(p.ImportPath, ".test") {
			continue // the generated test main is not consumer source
		}
		var files []*ast.File
		for _, name := range p.CompiledGoFiles {
			if !filepath.IsAbs(name) {
				name = filepath.Join(p.Dir, name)
			}
			file, err := parser.ParseFile(fset, name, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			files = append(files, file)
		}
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Instances:  map[*ast.Ident]types.Instance{},
		}
		path, _, _ := strings.Cut(p.ImportPath, " [")
		config := types.Config{Importer: mappedImporter{imp, p.ImportMap}}
		if _, err := config.Check(path, fset, files, info); err != nil {
			return nil, errors.New("cannot type-check " + p.ImportPath + " against its current dependency: " + err.Error())
		}
		result = append(result, sourcePackage{path: path, files: files, info: info})
	}
	return result, nil
}
