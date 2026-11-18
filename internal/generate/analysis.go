package generate

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"strings"
)

// AnalysisPackage retains complete function bodies for read-only source tooling.
// Generation's declaration-only pass remains independent of this check.
type AnalysisPackage struct {
	Types            *types.Package
	TypesInfo        *types.Info
	Fset             *token.FileSet
	Syntax           []*ast.File
	HTTPDependencies map[string]bool
	Declarations     map[string]TransportDeclaration
}

// FrameworkHTTPDependencies uses the same source import graph as generation.
// It is also used by the external migration before any annotation is written.
func FrameworkHTTPDependencies(ctx context.Context, dir string) (map[string]bool, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	scope, err := loadModuleScope(ctx, root)
	if err != nil {
		return nil, err
	}
	g := &packageGraph{root: root, scope: scope}
	args := append([]string{"list"}, scope.listFlags()...)
	args = append(args, "-deps", "-json=ImportPath,Deps", "--", framework+"/http")
	if err := g.goList(ctx, args, func(pkg listedPackage) {
		if pkg.ImportPath == framework+"/http" {
			g.httpDependencies = httpDependencySet(pkg.Deps)
		}
	}); err != nil {
		return nil, err
	}
	return g.httpDependencies, nil
}

// LoadAnalysis reuses the generator's Go listing, module selection and export
// importer. Test variants use their own import remapping and type-check context.
func LoadAnalysis(ctx context.Context, dir string, patterns []string) (result []*AnalysisPackage, resultErr error) {
	defer func() {
		if ctx.Err() != nil {
			result = nil
			resultErr = ctx.Err()
		}
	}()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if dir == "" {
		dir = "."
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	scope, err := loadModuleScope(ctx, root)
	if err != nil {
		return nil, err
	}
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "-") {
			return nil, fmt.Errorf("package patterns cannot be flags")
		}
	}
	g := &packageGraph{root: root, scope: scope, exports: make(map[string]string)}
	args := append([]string{"list"}, scope.listFlags()...)
	args = append(args, "-deps", "-test", "-compiled", "-export", "-json=Dir,ImportPath,Name,GoFiles,CompiledGoFiles,ImportMap,DepOnly,Deps,Export,Module,Error", "--")
	args = append(args, patterns...)
	var selected []listedPackage
	var own []listedPackage
	if err := g.goList(ctx, args, func(pkg listedPackage) {
		if pkg.Module != nil && pkg.Module.Path == scope.path && scope.path != framework {
			own = append(own, pkg)
		}
		if pkg.Export != "" {
			g.exports[pkg.ImportPath] = pkg.Export
		}
		if pkg.ImportPath == framework+"/http" {
			g.httpDependencies = httpDependencySet(pkg.Deps)
		}
		if !pkg.DepOnly && !(pkg.Name == "main" && strings.HasSuffix(pkg.ImportPath, ".test")) {
			selected = append(selected, pkg)
		}
	}); err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no Go packages matched")
	}
	declarations := make(map[string]TransportDeclaration)
	metadataFiles := make(map[string]bool)
	for _, pkg := range own {
		path, _, _ := strings.Cut(pkg.ImportPath, " [")
		for _, name := range pkg.GoFiles {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !filepath.IsAbs(name) {
				name = filepath.Join(pkg.Dir, name)
			}
			if metadataFiles[name] {
				continue
			}
			metadataFiles[name] = true
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			for _, d := range transportDeclarations(path, fset, file) {
				declarations[d.Key] = d
			}
		}
	}
	for _, pkg := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		exports := maps.Clone(g.exports)
		for original, variant := range pkg.ImportMap {
			exports[original] = g.exports[variant]
		}
		imports := &packageGraph{fset: fset, exports: exports}
		analysis := &AnalysisPackage{Declarations: declarations, Fset: fset, HTTPDependencies: g.httpDependencies, TypesInfo: &types.Info{
			Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object),
			Instances: make(map[*ast.Ident]types.Instance), Selections: make(map[*ast.SelectorExpr]*types.Selection), Implicits: make(map[ast.Node]types.Object),
		}}
		files := pkg.CompiledGoFiles
		if len(files) == 0 {
			files = pkg.GoFiles
		}
		for _, name := range files {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !filepath.IsAbs(name) {
				name = filepath.Join(pkg.Dir, name)
			}
			file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			analysis.Syntax = append(analysis.Syntax, file)
		}
		config := types.Config{Importer: imports.exportImporter()}
		path, _, _ := strings.Cut(pkg.ImportPath, " [")
		analysis.Types, err = config.Check(path, fset, analysis.Syntax, analysis.TypesInfo)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", path, err)
		}
		result = append(result, analysis)
	}
	return result, nil
}
