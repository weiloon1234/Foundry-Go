package generate

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Same-package tests reserve names across build configurations. External test
// packages keep their own namespace and are checked by the Go compiler.
func (p *packageInput) testSymbols(ctx context.Context, g *packageGraph) error {
	p.testDefinedAt = make(map[string]token.Pos)
	imports := make(map[string][]token.Pos)
	for _, name := range p.goNames {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.dir, name))
		if err != nil {
			return err
		}
		clause, err := parser.ParseFile(p.fset, p.displayName(name), data, parser.PackageClauseOnly)
		if err != nil {
			return err
		}
		if clause.Name.Name != p.name {
			continue
		}
		file, err := parser.ParseFile(p.fset, p.displayName(name), data, parser.ParseComments)
		if err != nil {
			return err
		}
		p.testFiles = append(p.testFiles, source{name, data, file})
		record := func(id *ast.Ident) {
			if id.Name != "_" {
				p.testDefinedAt[id.Name] = id.Pos()
			}
		}
		for _, spec := range file.Imports {
			if spec.Name != nil {
				record(spec.Name)
				continue
			}
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			imports[path] = append(imports[path], spec.Pos())
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					record(d.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						record(s.Name)
					case *ast.ValueSpec:
						for _, id := range s.Names {
							record(id)
						}
					}
				}
			}
		}
	}
	// Reuse package metadata already loaded for generation; only test-only imports
	// need a name-only Go listing (no test compilation or export-data request).
	names := make(map[string]string)
	var missing []string
	for _, path := range sortedNames(imports) {
		pkg := g.packages[path]
		if pkg.Name == "" {
			pkg = g.documentationPackages[path]
		}
		if pkg.Name != "" {
			names[path] = pkg.Name
		} else {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		args := append([]string{"list"}, g.scope.listFlags()...)
		args = append(args, "-e", "-find", "-json=ImportPath,Name", "--")
		args = append(args, missing...)
		if err := g.goList(ctx, args, func(pkg listedPackage) { names[pkg.ImportPath] = pkg.Name }); err != nil {
			return err
		}
	}
	for _, path := range sortedNames(imports) {
		name := names[path]
		if name == "" {
			return p.diagnostic(imports[path][0], fmt.Sprintf("cannot resolve default test import name for %s; use an explicit import alias", path))
		}
		p.testDefinedAt[name] = imports[path][0]
	}
	return nil
}
