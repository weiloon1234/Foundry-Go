package codemod

import (
	"go/ast"
	"go/types"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

const (
	httpPath         = frameworkinfo.ModulePath + "/http"
	paginationPath   = httpPath + "/pagination"
	modelbindingPath = httpPath + "/modelbinding"
)

// HTTP migrates declarations using the consumer's imported v0.1 objects.
type HTTP struct{}
type httpFamily struct{ path, name string }
type httpRewrite struct{ target, option string }

func httpMethod(selection *types.Selection) (httpRewrite, bool) {
	if selection == nil {
		return httpRewrite{}, false
	}
	method, ok := selection.Obj().(*types.Func)
	if !ok || method.Pkg() == nil {
		return httpRewrite{}, false
	}
	origin := method.Origin()
	signature, ok := origin.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return httpRewrite{}, false
	}
	receiver := types.Unalias(signature.Recv().Type())
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = types.Unalias(pointer.Elem())
	}
	named, ok := receiver.(*types.Named)
	if !ok || named.Obj().Pkg() != method.Pkg() {
		return httpRewrite{}, false
	}
	rules := httpMethods[httpFamily{method.Pkg().Path(), named.Obj().Name()}]
	rule, ok := rules[origin.Name()]
	if !ok {
		return httpRewrite{}, false
	}
	for i := 0; i < named.NumMethods(); i++ {
		if named.Method(i).Origin() == origin {
			return rule, true
		}
	}
	return httpRewrite{}, false
}

func httpRemovedMethod(selection *types.Selection) (string, bool) {
	rule, ok := httpMethod(selection)
	return rule.target, ok
}

func (HTTP) Rewrite(fc *FileContext, node ast.Node) (ast.Node, string) {
	if fc.httpReplay == nil {
		fc.httpReplay = map[ast.Node]httpReplayEdit{}
		fc.httpReplayNames = map[string]bool{}
	}
	if edit, ok := fc.httpReplay[node]; ok {
		return edit.node, edit.message
	}
	if block, ok := node.(*ast.BlockStmt); ok {
		return rewriteHTTPReplayBlock(fc, block)
	}
	if call, ok := node.(*ast.CallExpr); ok {
		if function := httpPackageFunction(fc, call.Fun); function != nil && function.Pkg().Path() == modelbindingPath {
			switch function.Name() {
			case "Bind", "BindAuthenticated":
				return rewriteHTTPBinding(fc, call, function.Name())
			case "Idempotent", "IdempotentAuthenticated":
				return rewriteHTTPReplay(fc, call)
			}
		}
	}
	if message := methodInterfaceBoundary(fc, node, httpRemovedMethod, "HTTP"); message != "" {
		return nil, message
	}
	// Explicit old transport contracts need a decision about binding timing.
	if id, ok := node.(*ast.Ident); ok {
		if function, ok := fc.Info.Uses[id].(*types.Func); ok && function.Pkg() != nil && function.Pkg().Path() == modelbindingPath && function.Pkg().Scope().Lookup(function.Name()) == function {
			switch function.Name() {
			case "Bind", "BindAuthenticated", "Idempotent", "IdempotentAuthenticated":
				if !httpDirectFunctionCall(fc, id) {
					return nil, "stored modelbinding constructor requires an explicit binding adapter"
				}
			}
		}
		if obj, ok := fc.Info.Uses[id].(*types.TypeName); ok && obj.Pkg() != nil && obj.Pkg().Scope().Lookup(obj.Name()) == obj {
			if obj.Pkg().Path() == modelbindingPath && obj.Name() == "Transport" || obj.Pkg().Path() == httpPath && obj.Name() == "AuthenticatedTransport" {
				return nil, "HTTP transport interface requires an explicit binding capability or handler contract"
			}
		}
	}
	var selector *ast.SelectorExpr
	var call *ast.CallExpr
	switch n := node.(type) {
	case *ast.CallExpr:
		call = n
		selector, _ = unparen(n.Fun).(*ast.SelectorExpr)
	case *ast.SelectorExpr:
		selector = n
		parent := fc.Parents[node]
		for {
			p, ok := parent.(*ast.ParenExpr)
			if !ok {
				break
			}
			parent = fc.Parents[p]
		}
		if c, ok := parent.(*ast.CallExpr); ok && unparen(c.Fun) == n {
			return nil, ""
		}
	default:
		return nil, ""
	}
	if selector == nil {
		return nil, ""
	}
	selection := fc.Info.Selections[selector]
	rule, ok := httpMethod(selection)
	if !ok {
		return nil, ""
	}
	if rule.option == "" {
		return rewriteSelectedMethod(fc, selector, call, selection, rule.target, selection.Obj().Pkg().Path())
	}
	message := selection.Obj().Name() + " → " + rule.target
	if call == nil || selection.Kind() != types.MethodVal {
		return nil, message + ": configuration callback requires a typed option-adapting closure"
	}
	if len(selection.Index()) != 1 {
		return nil, message + ": promoted receiver requires an explicit adapter"
	}
	if _, ok := types.Unalias(selection.Recv()).(*types.Named); !ok {
		return nil, message + ": pointer or unsupported receiver requires an explicit snapshot"
	}
	if effectfulArguments(call.Args) {
		return nil, message + ": option argument effects require manual evaluation-order review"
	}
	if len(call.Args) == 1 {
		if _, ok := fc.Info.TypeOf(call.Args[0]).(*types.Tuple); ok {
			return nil, message + ": multi-result argument requires manual expansion"
		}
	}
	alias, ok := packageAlias(fc, selection.Obj().Pkg().Path(), selector.Pos())
	if !ok {
		return nil, message + ": missing, dot-imported or shadowed package import"
	}
	option, ok := httpOption(fc, call, selection, rule)
	if !ok {
		return nil, message + ": option type or import cannot be named safely"
	}
	options := []ast.Expr{option}
	base := selector.X
	// Flatten only contiguous options for the same owner. Typed functions and
	// authentication transitions remain boundaries; their nesting is retained.
	for {
		inner, ok := unparen(base).(*ast.CallExpr)
		if !ok {
			break
		}
		innerSelector, ok := unparen(inner.Fun).(*ast.SelectorExpr)
		if !ok {
			break
		}
		innerSelection := fc.Info.Selections[innerSelector]
		innerRule, ok := httpMethod(innerSelection)
		if !ok || innerRule.option == "" || innerRule.target != rule.target || innerSelection.Obj().Pkg() != selection.Obj().Pkg() || innerSelection.Kind() != types.MethodVal || len(innerSelection.Index()) != 1 || effectfulArguments(inner.Args) {
			break
		}
		if _, ok := types.Unalias(innerSelection.Recv()).(*types.Named); !ok {
			break
		}
		next, ok := httpOption(fc, inner, innerSelection, innerRule)
		if !ok {
			break
		}
		options = append([]ast.Expr{next}, options...)
		base = innerSelector.X
	}
	if constructor, ok := unparen(base).(*ast.CallExpr); ok && !constructor.Ellipsis.IsValid() && !effectfulArguments(constructor.Args) {
		function := httpPackageFunction(fc, constructor.Fun)
		if function != nil && httpOptionConstructor(function, rule.target, selection.Obj().Pkg().Path()) {
			result := *constructor
			result.Args = append(append([]ast.Expr{}, constructor.Args...), options...)
			result.Rparen = call.Rparen
			return &result, message + " (ordered constructor options)"
		}
	}
	return &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(rule.target)}, Args: append([]ast.Expr{base}, options...), Lparen: call.Lparen, Rparen: call.Rparen}, message
}

func httpDirectFunctionCall(fc *FileContext, id *ast.Ident) bool {
	var node ast.Node = id
	for {
		parent := fc.Parents[node]
		switch parent := parent.(type) {
		case *ast.SelectorExpr:
			if parent.Sel != node {
				return false
			}
		case *ast.IndexExpr:
			if parent.X != node {
				return false
			}
		case *ast.IndexListExpr:
			if parent.X != node {
				return false
			}
		case *ast.ParenExpr:
		case *ast.CallExpr:
			return parent.Fun == node
		default:
			return false
		}
		node = parent
	}
}

func httpOption(fc *FileContext, call *ast.CallExpr, selection *types.Selection, rule httpRewrite) (ast.Expr, bool) {
	alias, ok := packageAlias(fc, httpPath, call.Pos())
	if !ok {
		return nil, false
	}
	var function ast.Expr = &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(rule.option)}
	if rule.option == "ActorMiddleware" || rule.option == "Permissions" && len(call.Args) == 0 {
		named, ok := types.Unalias(selection.Recv()).(*types.Named)
		if !ok {
			return nil, false
		}
		index := 3
		switch named.Obj().Name() {
		case "AuthenticatedRoute", "OptionalAuthenticationRoute":
			index = 1
		}
		if named.TypeArgs().Len() <= index {
			return nil, false
		}
		actor, ok := typeExpr(fc, named.TypeArgs().At(index), call.Pos())
		if !ok {
			return nil, false
		}
		anchorTypePositions(reflect.ValueOf(actor), call.Pos())
		function = &ast.IndexExpr{X: function, Index: actor, Lbrack: call.Pos(), Rbrack: call.Pos()}
	}
	return &ast.CallExpr{Fun: function, Args: call.Args, Ellipsis: call.Ellipsis, Lparen: call.Lparen, Rparen: call.Rparen}, true
}

func httpPackageFunction(fc *FileContext, expr ast.Expr) *types.Func {
	expr = unparen(expr)
	switch indexed := expr.(type) {
	case *ast.IndexExpr:
		expr = unparen(indexed.X)
	case *ast.IndexListExpr:
		expr = unparen(indexed.X)
	}
	var id *ast.Ident
	switch expr := expr.(type) {
	case *ast.Ident:
		id = expr
	case *ast.SelectorExpr:
		if fc.Info.Selections[expr] != nil {
			return nil
		}
		id = expr.Sel
	}
	if id == nil {
		return nil
	}
	function, ok := fc.Info.Uses[id].(*types.Func)
	if !ok || function.Pkg() == nil || function.Pkg().Scope().Lookup(function.Name()) != function {
		return nil
	}
	return function
}

func httpOptionConstructor(function *types.Func, target, path string) bool {
	if function.Pkg().Path() != path {
		return false
	}
	name := function.Name()
	if path == paginationPath {
		return target == "Configure" && (name == "DefineNumbered" || name == "DefineSimple" || name == "DefineCursor") || target == "ConfigureAuthenticated" && name == "Authenticated"
	}
	if path != httpPath {
		return false
	}
	switch target {
	case "ConfigureEndpoint":
		return name == "DefineEndpoint"
	case "ConfigureRoute":
		return name == "DefineRoute"
	case "ConfigureAuthenticated":
		return name == "Authenticated" || name == "RequireAuthentication"
	case "ConfigureOptional":
		return name == "OptionallyAuthenticated" || name == "OptionalAuthentication"
	case "ConfigureAuthenticatedRoute":
		return name == "RequireRouteAuthentication"
	case "ConfigureOptionalRoute":
		return name == "OptionalRouteAuthentication"
	}
	return false
}

func rewriteHTTPBinding(fc *FileContext, call *ast.CallExpr, name string) (ast.Node, string) {
	message := "modelbinding." + name + " requires an explicit binding transport"
	if len(call.Args) != 2 {
		return nil, message + ": unsupported argument form"
	}
	named, ok := types.Unalias(fc.Info.TypeOf(call.Args[0])).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != httpPath || named.Obj().Pkg().Scope().Lookup(named.Obj().Name()) != named.Obj() {
		return nil, message + ": custom or indirect transport; choose staged binding or AfterValidation explicitly"
	}
	adapter := ""
	if name == "Bind" {
		switch named.Obj().Name() {
		case "Endpoint":
			adapter = "ForBinding"
		case "SignedEndpoint":
			adapter = "ForSignedBinding"
		}
	} else {
		switch named.Obj().Name() {
		case "AuthenticatedEndpoint":
			adapter = "ForAuthenticatedBinding"
		case "OptionalAuthenticationEndpoint":
			adapter = "ForOptionalBinding"
		case "SignedAuthenticatedEndpoint":
			adapter = "ForSignedAuthenticatedBinding"
			if named.TypeArgs().Len() > 3 {
				actor, ok := types.Unalias(named.TypeArgs().At(3)).(*types.Named)
				if ok && actor.Obj().Pkg() != nil && actor.Obj().Pkg().Path() == frameworkinfo.ModulePath+"/value" && actor.Obj().Pkg().Scope().Lookup("Optional") == actor.Obj() {
					adapter = "ForSignedOptionalBinding"
				}
			}
		}
	}
	if adapter == "" {
		return nil, message + ": unsupported endpoint family"
	}
	alias, ok := packageAlias(fc, httpPath, call.Pos())
	if !ok {
		return nil, message + ": dot-imported or shadowed HTTP import"
	}
	result := *call
	result.Args = append([]ast.Expr{}, call.Args...)
	result.Args[0] = &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(adapter)}, Args: []ast.Expr{call.Args[0]}}
	return &result, "modelbinding." + name + " → http." + adapter
}
