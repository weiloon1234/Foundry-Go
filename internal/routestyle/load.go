// Package routestyle checks typed route declarations without changing request execution.
package routestyle

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

// Load includes test sources and honours the selected module's build configuration.
// Loading failures are errors, never an empty successful check.
func Load(ctx context.Context, dir string, patterns []string) ([]*generate.AnalysisPackage, error) {
	return generate.LoadAnalysis(ctx, dir, patterns)
}

func check(ctx context.Context, dir string, patterns []string) (result []diagnostic.Diagnostic, resultErr error) {
	defer func() {
		if ctx.Err() != nil {
			result = nil
			resultErr = ctx.Err()
		}
	}()
	pkgs, err := Load(ctx, dir, patterns)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, pkg := range pkgs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		a := NewAnalysis(pkg.TypesInfo, pkg.Fset)
		a.Context = ctx
		a.HTTPDependencies = pkg.HTTPDependencies
		for _, file := range pkg.Syntax {
			a.Index(file)
		}
		a.Declarations = pkg.Declarations
		for _, file := range pkg.Syntax {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			for _, d := range a.CheckFile(file) {
				key := d.String()
				if !seen[key] {
					seen[key] = true
					result = append(result, d)
				}
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Rule.ID < b.Rule.ID
	})
	return result, nil
}

// Analysis retains one package's type information and function-alias definitions.
type Analysis struct {
	Context          context.Context
	Info             *types.Info
	Files            *token.FileSet
	aliases          map[types.Object][]ast.Expr
	stages           map[*types.Package]map[*types.TypeName]bool
	HTTPDependencies map[string]bool
	Declarations     map[string]generate.TransportDeclaration
}

func NewAnalysis(info *types.Info, files *token.FileSet) *Analysis {
	return &Analysis{Context: context.Background(), stages: make(map[*types.Package]map[*types.TypeName]bool), Info: info, Files: files, aliases: make(map[types.Object][]ast.Expr), Declarations: make(map[string]generate.TransportDeclaration)}
}
func (a *Analysis) Index(file *ast.File) {
	if a.Context.Err() != nil {
		return
	}
	for _, d := range generate.TransportDeclarations(a.Info, a.Files, file) {
		a.Declarations[d.Key] = d
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if a.Context.Err() != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.ValueSpec:
			if len(n.Names) == len(n.Values) {
				for i, id := range n.Names {
					a.aliases[a.Info.ObjectOf(id)] = append(a.aliases[a.Info.ObjectOf(id)], n.Values[i])
				}
			}
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i, left := range n.Lhs {
					if id, ok := left.(*ast.Ident); ok {
						a.aliases[a.Info.ObjectOf(id)] = append(a.aliases[a.Info.ObjectOf(id)], n.Rhs[i])
					}
				}
			}
		}
		return true
	})
}

// Function resolves direct references and aliases whose assignments all select
// the same function. Ambiguous function values are not guessed.
func (a *Analysis) Function(expr ast.Expr) *types.Func {
	return a.function(expr, make(map[types.Object]bool))
}
func (a *Analysis) function(expr ast.Expr, seen map[types.Object]bool) *types.Func {
	if a.Context.Err() != nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if len(e.Args) == 1 && a.Info.Types[e.Fun].IsType() {
			if typ := a.Info.TypeOf(e); typ != nil {
				if _, ok := typ.Underlying().(*types.Signature); ok {
					return a.function(e.Args[0], seen)
				}
			}
		}
	case *ast.ParenExpr:
		return a.function(e.X, seen)
	case *ast.IndexExpr:
		return a.function(e.X, seen)
	case *ast.IndexListExpr:
		return a.function(e.X, seen)
	case *ast.SelectorExpr:
		fn, _ := a.Info.ObjectOf(e.Sel).(*types.Func)
		return fn
	case *ast.Ident:
		object := a.Info.ObjectOf(e)
		if fn, ok := object.(*types.Func); ok {
			return fn
		}
		if object == nil || seen[object] {
			return nil
		}
		seen[object] = true
		defer delete(seen, object)
		var fn *types.Func
		for _, value := range a.aliases[object] {
			next := a.function(value, seen)
			if next == nil || (fn != nil && fn != next) {
				return nil
			}
			fn = next
		}
		return fn
	}
	return nil
}
