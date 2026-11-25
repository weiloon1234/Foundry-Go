package routestyle

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/routecontract"
)

func (a *Analysis) CheckFile(file *ast.File) []diagnostic.Diagnostic {
	var result []diagnostic.Diagnostic
	generated := ast.IsGenerated(file)
	ast.Inspect(file, func(n ast.Node) bool {
		if a.Context.Err() != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !generated {
			if use, ok := a.Use(call); ok && !a.HTTPDependencies[use.Owner.Obj().Pkg().Path()] {
				rule := diagnostic.RouteHelperMissing
				if _, exists := a.Helper(call); exists {
					rule = diagnostic.RouteHelper
				}
				position := a.Files.Position(call.Pos())
				result = append(result, rule.At(position.Filename, position.Line, position.Column).
					Say(a.expression(call.Fun), "wraps the generated declaration of "+use.Owner.Obj().Name()).
					To(fmt.Sprintf("declare http=%s (or http=both) on %s, run foundry generate, then call %s", use.Mode(), use.Owner.Obj().Name(), use.Owner.Obj().Pkg().Name()+"."+use.Name)))
			}
		}
		for _, arg := range call.Args {
			ast.Inspect(arg, func(n ast.Node) bool {
				if a.Context.Err() != nil {
					return false
				}
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				if inner, ok := n.(*ast.CallExpr); ok {
					if !a.Info.Types[inner.Fun].IsType() && a.IsStage(a.Info.TypeOf(inner)) {
						position := a.Files.Position(inner.Pos())
						result = append(result, diagnostic.RouteNesting.At(position.Filename, position.Line, position.Column).
							Say(a.expression(inner.Fun)+"(...)", "route stage inside another call").
							To("assign it to a local, then pass the local"))
					}
					// Each call's arguments are checked by the outer walk. Its receiver
					// is not an argument: stage.Handle(...) and stage.URL(...) stay inline.
					return false
				}
				return true
			})
		}
		return true
	})
	return result
}

// expression prints an expression as written, for a diagnostic subject.
func (a *Analysis) expression(expr ast.Expr) string {
	var text bytes.Buffer
	printer.Fprint(&text, a.Files, expr)
	return text.String()
}

// Helper describes a replacement using the descriptor's existing package qualifier.
// Arguments retain their original evaluation order. Only concrete generated
// JSON and multipart declarations with an existing, exactly typed helper qualify.
type Helper struct {
	Name      string
	Function  ast.Expr
	Arguments []ast.Expr
}

func (a *Analysis) Use(call *ast.CallExpr) (TransportUse, bool) {
	fn := a.Function(call.Fun)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != frameworkinfo.ModulePath+"/http" {
		return TransportUse{}, false
	}
	kind, index, suffix := fn.Name(), 0, "Body"
	switch kind {
	case "JSONBody", "MultipartBody":
	case "JSONResponse":
		index, suffix = 1, "Response"
	default:
		return TransportUse{}, false
	}
	if len(call.Args) != index+1 {
		return TransportUse{}, false
	}
	descriptor, ok := unparen(call.Args[index]).(*ast.CallExpr)
	if !ok {
		return TransportUse{}, false
	}
	var args []ast.Expr
	if kind == "JSONResponse" {
		args = []ast.Expr{call.Args[0]}
	}
	if kind == "MultipartBody" {
		args = []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `""`}}
		if wrapper := a.Function(descriptor.Fun); wrapper != nil && wrapper.Pkg() != nil && wrapper.Pkg().Path() == frameworkinfo.ModulePath+"/http" && wrapper.Name() == "MultipartDirectory" && len(descriptor.Args) == 2 {
			args[0] = descriptor.Args[1]
			descriptor, ok = unparen(descriptor.Args[0]).(*ast.CallExpr)
			if !ok {
				return TransportUse{}, false
			}
		}
	}
	decl := a.Function(descriptor.Fun)
	if decl == nil || decl.Pkg() == nil || len(descriptor.Args) != 0 {
		return TransportUse{}, false
	}
	if !strings.HasSuffix(a.Files.Position(decl.Pos()).Filename, "_foundry.gen.go") {
		return TransportUse{}, false
	}
	sig := decl.Type().(*types.Signature)
	if sig.TypeParams().Len() != 0 || sig.Results().Len() != 1 {
		return TransportUse{}, false
	}
	result, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
	if !ok || result.TypeArgs().Len() != 1 {
		return TransportUse{}, false
	}
	owner, ok := types.Unalias(result.TypeArgs().At(0)).(*types.Named)
	if !ok || owner.TypeArgs().Len() != 0 {
		return TransportUse{}, false
	}
	name := routecontract.HelperName(owner.Obj().Name(), suffix)
	key := owner.Obj().Pkg().Path() + "." + owner.Obj().Name()
	declaration, exists := a.Declarations[key]
	if !exists || (declaration.Kind != "dto" && declaration.Kind != "multipart") || declaration.Generic {
		return TransportUse{}, false
	}
	return TransportUse{Owner: owner, Descriptor: decl, Expression: descriptor, Kind: kind, Name: name, Arguments: args}, true
}

type TransportUse struct {
	Owner      *types.Named
	Descriptor *types.Func
	Expression *ast.CallExpr
	Kind, Name string
	Arguments  []ast.Expr
}

func (u TransportUse) Key() string { return u.Owner.Obj().Pkg().Path() + "." + u.Owner.Obj().Name() }
func (u TransportUse) Mode() string {
	if u.Kind == "JSONResponse" {
		return "response"
	}
	return "body"
}

func (a *Analysis) Helper(call *ast.CallExpr) (Helper, bool) {
	use, ok := a.Use(call)
	if !ok {
		return Helper{}, false
	}
	decl, descriptor, kind, name, args := use.Descriptor, use.Expression, use.Kind, use.Name, use.Arguments

	helper, ok := decl.Pkg().Scope().Lookup(name).(*types.Func)
	if !ok || !strings.HasSuffix(a.Files.Position(helper.Pos()).Filename, "_foundry.gen.go") {
		return Helper{}, false
	}
	hs := helper.Type().(*types.Signature)
	if hs.TypeParams().Len() != 0 || hs.Params().Len() != len(args) || hs.Results().Len() != 1 || !types.Identical(hs.Results().At(0).Type(), a.Info.TypeOf(call)) {
		return Helper{}, false
	}
	if len(args) == 1 {
		parameter := types.Typ[types.Int]
		if kind == "MultipartBody" {
			parameter = types.Typ[types.String]
		}
		if !types.Identical(hs.Params().At(0).Type(), parameter) {
			return Helper{}, false
		}
	}
	var fun ast.Expr
	switch expression := unparen(descriptor.Fun).(type) {
	case *ast.SelectorExpr:
		fun = &ast.SelectorExpr{X: expression.X, Sel: ast.NewIdent(name)}
	case *ast.Ident:
		if a.Info.ObjectOf(expression) == decl {
			fun = ast.NewIdent(name)
		}
	default:
		return Helper{}, false
	}
	return Helper{Name: decl.Pkg().Name() + "." + name, Function: fun, Arguments: args}, true
}

func unparen(expression ast.Expr) ast.Expr {
	for {
		p, ok := expression.(*ast.ParenExpr)
		if !ok {
			return expression
		}
		expression = p.X
	}
}
