package codemod

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"reflect"
)

type httpReplayEdit struct {
	node    ast.Node
	message string
}

type httpReplaySource struct {
	bind               *ast.CallExpr
	endpoint, resolver ast.Expr
	policies           []*ast.CallExpr
}

type httpReplayPlan struct {
	expression ast.Expr
	prefixes   []ast.Stmt
	policies   []ast.Expr
	ordinary   ast.Expr
}

func httpReplayConstructor(fc *FileContext, call *ast.CallExpr) string {
	function := httpPackageFunction(fc, call.Fun)
	if function == nil || function.Pkg().Path() != modelbindingPath {
		return ""
	}
	switch function.Name() {
	case "Idempotent", "IdempotentAuthenticated":
		return function.Name()
	}
	return ""
}

// Local declaration rewrites can introduce ordinary Go statements instead of
// capture closures. All decisions still use the original consumer type info.
func rewriteHTTPReplayBlock(fc *FileContext, block *ast.BlockStmt) (ast.Node, string) {
	result := *block
	result.List = nil
	changed := false
	for i, statement := range block.List {
		assignment, ok := statement.(*ast.AssignStmt)
		if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			result.List = append(result.List, statement)
			continue
		}
		id, ok := assignment.Lhs[0].(*ast.Ident)
		if !ok || id.Name == "_" || fc.Info.Defs[id] == nil {
			result.List = append(result.List, statement)
			continue
		}
		call, headers := httpReplayRoot(fc, assignment.Rhs[0])
		if call == nil {
			result.List = append(result.List, statement)
			continue
		}
		imports := maps.Clone(fc.AddedImports)
		names := maps.Clone(fc.httpReplayNames)
		sourceExpr := call.Args[0]
		var saved *ast.AssignStmt
		var savedID *ast.Ident
		if name, ok := unparen(sourceExpr).(*ast.Ident); ok && i > 0 {
			previous, ok := block.List[i-1].(*ast.AssignStmt)
			if ok && previous.Tok == token.DEFINE && len(previous.Lhs) == 1 && len(previous.Rhs) == 1 {
				declared, ok := previous.Lhs[0].(*ast.Ident)
				if ok && fc.Info.Defs[declared] != nil && fc.Info.Uses[name] == fc.Info.Defs[declared] && len(result.List) > 0 && result.List[len(result.List)-1] == previous {
					saved, savedID, sourceExpr = previous, declared, previous.Rhs[0]
				}
			}
		}
		// Splitting the initializer brings the new name into scope earlier. Reject
		// direct references that would then bind to it instead of an outer variable.
		shadowed := false
		for _, header := range headers {
			shadowed = shadowed || httpReplayMentions(header.Args, id.Name)
		}
		source, reason := httpReplayBinding(fc, sourceExpr, httpReplayConstructor(fc, call))
		if reason == "" {
			for _, policy := range source.policies {
				shadowed = shadowed || httpReplayMentions(policy.Args, id.Name)
			}
		}
		if reason != "" || shadowed {
			fc.AddedImports = imports
			fc.httpReplayNames = names
			result.List = append(result.List, statement)
			continue
		}
		alias, imported := packageAlias(fc, modelbindingPath, call.Pos())
		if !imported || (len(source.policies)+len(headers) > 0 && alias == id.Name) {
			fc.AddedImports = imports
			fc.httpReplayNames = names
			result.List = append(result.List, statement)
			continue
		}
		shared := savedID != nil && httpReplayUses(fc, fc.Info.Defs[savedID]) > 1
		// An escaping address would allow the saved declaration to be changed during
		// evaluation of the store/definition. Such aliasing needs consumer review.
		if shared && httpReplayAddressed(fc, fc.Info.Defs[savedID]) {
			fc.AddedImports = imports
			fc.httpReplayNames = names
			result.List = append(result.List, statement)
			continue
		}
		plan, reason := planHTTPReplay(fc, call, source, true, shared)
		if reason != "" {
			fc.AddedImports = imports
			fc.httpReplayNames = names
			fc.httpReplay[call] = httpReplayEdit{message: "modelbinding replay: " + reason}
			result.List = append(result.List, statement)
			continue
		}
		if saved != nil {
			result.List = result.List[:len(result.List)-1]
		}
		if shared {
			// Snapshot the binding inputs at the saved declaration, then evaluate the
			// replay arguments after that declaration is available to their callbacks.
			boundary := len(plan.prefixes) - 2
			result.List = append(result.List, plan.prefixes[:boundary]...)
			value := plan.ordinary
			helper := "AuthorizeResource"
			if httpReplayConstructor(fc, call) == "IdempotentAuthenticated" {
				helper = "AuthorizeAuthenticatedResource"
			}
			for _, policy := range plan.policies {
				value = httpReplayCall(alias, helper, value, policy)
			}
			keep := *saved
			keep.Rhs = []ast.Expr{value}
			result.List = append(result.List, &keep)
			result.List = append(result.List, plan.prefixes[boundary:]...)
		} else {
			result.List = append(result.List, plan.prefixes...)
		}
		declaration := *assignment
		if saved != nil && !shared {
			name := *id
			name.NamePos = saved.Pos()
			declaration.Lhs = []ast.Expr{&name}
			declaration.TokPos = saved.TokPos
		}
		declaration.Rhs = []ast.Expr{plan.expression}
		result.List = append(result.List, &declaration)
		policyHelper, headerHelper := "AuthorizeIdempotentResource", "IdempotentHeaders"
		if httpReplayConstructor(fc, call) == "IdempotentAuthenticated" {
			policyHelper, headerHelper = "AuthorizeAuthenticatedIdempotentResource", "AuthenticatedIdempotentHeaders"
		}
		for _, policy := range plan.policies {
			result.List = append(result.List, httpReplayAssignment(id, httpReplayCall(alias, policyHelper, ast.NewIdent(id.Name), policy)))
		}
		for _, header := range headers {
			result.List = append(result.List, httpReplayAssignment(id, httpReplayCall(alias, headerHelper, ast.NewIdent(id.Name), header.Args[0])))
			fc.httpReplay[header] = httpReplayEdit{}
		}
		claimHTTPReplay(fc, call, source, plan.expression)
		changed = true
	}
	if !changed {
		return nil, ""
	}
	return &result, "explicit replay binding declarations"
}

func httpReplayRoot(fc *FileContext, expression ast.Expr) (*ast.CallExpr, []*ast.CallExpr) {
	var headers []*ast.CallExpr
	for {
		call, ok := unparen(expression).(*ast.CallExpr)
		if !ok {
			return nil, nil
		}
		if httpReplayConstructor(fc, call) != "" && len(call.Args) == 3 {
			return call, headers
		}
		selector, ok := unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || len(call.Args) != 1 {
			return nil, nil
		}
		selection := fc.Info.Selections[selector]
		rule, ok := httpMethod(selection)
		if !ok || selection.Kind() != types.MethodVal || len(selection.Index()) != 1 || selection.Obj().Pkg().Path() != modelbindingPath || (rule.target != "IdempotentHeaders" && rule.target != "AuthenticatedIdempotentHeaders") {
			return nil, nil
		}
		if _, ok := types.Unalias(selection.Recv()).(*types.Named); !ok {
			return nil, nil
		}
		headers = append([]*ast.CallExpr{call}, headers...)
		expression = selector.X
	}
}

func httpReplayBinding(fc *FileContext, expression ast.Expr, constructor string) (httpReplaySource, string) {
	source := httpReplaySource{}
	bindName, policyName := "Bind", "AuthorizeResource"
	if constructor == "IdempotentAuthenticated" {
		bindName, policyName = "BindAuthenticated", "AuthorizeAuthenticatedResource"
	}
	for {
		call, ok := unparen(expression).(*ast.CallExpr)
		if !ok {
			return source, "saved or indirect bound declaration requires adjacent local declaration review"
		}
		if function := httpPackageFunction(fc, call.Fun); function != nil && function.Pkg().Path() == modelbindingPath && function.Name() == bindName && len(call.Args) == 2 {
			source.bind, source.endpoint, source.resolver = call, call.Args[0], call.Args[1]
			return source, ""
		}
		selector, ok := unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || len(call.Args) != 1 {
			return source, "bound replay source is not a known binding declaration"
		}
		selection := fc.Info.Selections[selector]
		rule, ok := httpMethod(selection)
		if !ok || selection.Kind() != types.MethodVal || len(selection.Index()) != 1 || selection.Obj().Pkg().Path() != modelbindingPath || rule.target != policyName {
			return source, "bound policy requires an explicit resource adapter"
		}
		if _, ok := types.Unalias(selection.Recv()).(*types.Named); !ok {
			return source, "bound policy receiver requires an explicit snapshot"
		}
		source.policies = append([]*ast.CallExpr{call}, source.policies...)
		expression = selector.X
	}
}

func planHTTPReplay(fc *FileContext, call *ast.CallExpr, source httpReplaySource, statements, shared bool) (httpReplayPlan, string) {
	plan := httpReplayPlan{}
	named, ok := types.Unalias(fc.Info.TypeOf(source.endpoint)).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != httpPath || named.Obj().Pkg().Scope().Lookup(named.Obj().Name()) != named.Obj() {
		return plan, "custom or indirect transport was not supported by v0.1 replay; expose a concrete supported replay declaration explicitly"
	}
	transition, bind := "", "BindIdempotent"
	if httpReplayConstructor(fc, call) == "IdempotentAuthenticated" {
		bind = "BindAuthenticatedIdempotent"
		switch named.Obj().Name() {
		case "AuthenticatedEndpoint":
			transition = "IdempotentAuthenticated"
		case "OptionalAuthenticationEndpoint":
			return plan, "optional-actor replay was rejected at registration in v0.1; A1 provides no optional replay transition"
		case "SignedAuthenticatedEndpoint":
			return plan, "signed-actor replay (including signed optional actors) was rejected at registration in v0.1; A1 provides no signed-actor replay transition"
		}
	} else {
		switch named.Obj().Name() {
		case "Endpoint":
			transition = "Idempotent"
		case "SignedEndpoint":
			transition = "IdempotentSigned"
		}
	}
	if transition == "" {
		return plan, "endpoint family was not supported by v0.1 replay; A1 does not add that capability"
	}
	h, ok := packageAlias(fc, httpPath, call.Pos())
	if !ok {
		return plan, "dot-imported or shadowed HTTP import"
	}
	b, ok := packageAlias(fc, modelbindingPath, call.Pos())
	if !ok {
		return plan, "dot-imported or shadowed modelbinding import"
	}
	inputs := []ast.Expr{source.endpoint, source.resolver}
	for _, policy := range source.policies {
		inputs = append(inputs, policy.Args[0])
	}
	inputs = append(inputs, call.Args[1:]...)
	capture := shared
	for _, input := range inputs {
		capture = capture || !httpReplayStable(fc, input)
	}
	if capture && !statements {
		return plan, "reordered replay arguments have side effects or may panic; use a local declaration so inputs can be captured in order"
	}
	if capture {
		names := []string{"replayEndpoint", "replayResolver"}
		for range source.policies {
			names = append(names, "replayPolicy")
		}
		names = append(names, "replayStore", "replayDefinition")
		for i, expression := range inputs {
			typ := fc.Info.TypeOf(expression)
			basic, isBasic := typ.(*types.Basic)
			untyped := isBasic && basic.Info()&types.IsUntyped != 0
			nilValue := false
			if id, ok := unparen(expression).(*ast.Ident); ok {
				nilValue = fc.Info.Uses[id] == types.Universe.Lookup("nil")
			}
			if untyped || nilValue {
				switch {
				case i < 2:
					typ = fc.Info.TypeOf(source.bind.Fun).(*types.Signature).Params().At(i).Type()
				case i < 2+len(source.policies):
					selector := unparen(source.policies[i-2].Fun).(*ast.SelectorExpr)
					typ = fc.Info.Selections[selector].Type().(*types.Signature).Params().At(0).Type()
				default:
					typ = fc.Info.TypeOf(call.Fun).(*types.Signature).Params().At(i - len(inputs) + 3).Type()
				}
			}
			variable := freshName(fc, names[i])
			// Reserve generated locals as well as source names across this file.
			fc.httpReplayNames[variable] = true
			var statement ast.Stmt
			if untyped || nilValue || fc.Info.Types[expression].Value != nil {
				syntax, ok := typeExpr(fc, typ, call.Pos())
				if !ok {
					return plan, "capture type cannot be named safely"
				}
				anchorTypePositions(reflect.ValueOf(syntax), call.Pos())
				statement = &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(variable)}, Type: syntax, Values: []ast.Expr{expression}}}}}
			} else {
				statement = &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(variable)}, Tok: token.DEFINE, Rhs: []ast.Expr{expression}}
			}
			plan.prefixes = append(plan.prefixes, statement)
			inputs[i] = ast.NewIdent(variable)
		}
	}
	if !capture {
		for i, input := range inputs {
			inputs[i] = httpReplayArgument(input, call.Pos())
		}
	}
	plan.policies = inputs[2 : len(inputs)-2]
	copyBind := *source.bind
	copyBind.Args = []ast.Expr{inputs[0], inputs[1]}
	adapter := "ForBinding"
	if transition == "IdempotentSigned" {
		adapter = "ForSignedBinding"
	}
	if transition == "IdempotentAuthenticated" {
		adapter = "ForAuthenticatedBinding"
	}
	copyBind.Args[0] = httpReplayCall(h, adapter, inputs[0])
	plan.ordinary = &copyBind
	plan.expression = httpReplayCall(b, bind, httpReplayCall(h, transition, inputs[0], inputs[len(inputs)-2], inputs[len(inputs)-1]), inputs[1])
	return plan, ""
}

func rewriteHTTPReplay(fc *FileContext, call *ast.CallExpr) (ast.Node, string) {
	message := "modelbinding replay → explicit replay binding"
	if len(call.Args) != 3 {
		return nil, message + ": unsupported argument form"
	}
	source, reason := httpReplayBinding(fc, call.Args[0], httpReplayConstructor(fc, call))
	if reason != "" {
		return nil, message + ": " + reason
	}
	plan, reason := planHTTPReplay(fc, call, source, false, false)
	if reason != "" {
		return nil, message + ": " + reason
	}
	alias, _ := packageAlias(fc, modelbindingPath, call.Pos())
	helper := "AuthorizeIdempotentResource"
	if httpReplayConstructor(fc, call) == "IdempotentAuthenticated" {
		helper = "AuthorizeAuthenticatedIdempotentResource"
	}
	expression := plan.expression
	for _, policy := range plan.policies {
		expression = httpReplayCall(alias, helper, expression, policy)
	}
	claimHTTPReplay(fc, call, source, expression)
	return expression, message
}

func claimHTTPReplay(fc *FileContext, call *ast.CallExpr, source httpReplaySource, expression ast.Expr) {
	fc.httpReplay[call] = httpReplayEdit{expression, "modelbinding replay → explicit replay binding"}
	fc.httpReplay[source.bind] = httpReplayEdit{}
	for _, policy := range source.policies {
		fc.httpReplay[policy] = httpReplayEdit{}
	}
}

// Scalar reads in a synthesized call belong to that call's source line, not
// the removed binding declaration. Copy them so original type-info nodes remain
// intact. Keep compound syntax intact for nested rewrites and callback layout.
func httpReplayArgument(expression ast.Expr, pos token.Pos) ast.Expr {
	switch expression := expression.(type) {
	case *ast.Ident:
		result := *expression
		result.NamePos = pos
		return &result
	case *ast.BasicLit:
		result := *expression
		result.ValuePos = pos
		return &result
	}
	return expression
}

func httpReplayCall(alias, name string, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(name)}, Args: args}
}

func httpReplayAssignment(id *ast.Ident, expression ast.Expr) ast.Stmt {
	return &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(id.Name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{expression}}
}

func httpReplayMentions(expressions []ast.Expr, name string) bool {
	found := false
	for _, expression := range expressions {
		ast.Inspect(expression, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && id.Name == name {
				found = true
			}
			return !found
		})
	}
	return found
}

func httpReplayUses(fc *FileContext, object types.Object) int {
	count := 0
	for _, used := range fc.Info.Uses {
		if used == object {
			count++
		}
	}
	return count
}

func httpReplayAddressed(fc *FileContext, object types.Object) bool {
	for id, used := range fc.Info.Uses {
		if used != object {
			continue
		}
		var expression ast.Node = id
		for {
			parent, ok := fc.Parents[expression].(*ast.ParenExpr)
			if !ok {
				break
			}
			expression = parent
		}
		if unary, ok := fc.Parents[expression].(*ast.UnaryExpr); ok && unary.Op == token.AND {
			return true
		}
	}
	return false
}

// Only inert reads may move across the replay constructor. Indexing, pointer
// selection, division, calls and receives can make their evaluation order visible.
func httpReplayStable(fc *FileContext, expression ast.Expr) bool {
	switch expression := unparen(expression).(type) {
	case *ast.Ident, *ast.BasicLit, *ast.FuncLit:
		return true
	case *ast.SelectorExpr:
		id, ok := expression.X.(*ast.Ident)
		if !ok {
			return false
		}
		_, ok = fc.Info.Uses[id].(*types.PkgName)
		return ok
	case *ast.CompositeLit:
		if _, ok := fc.Info.TypeOf(expression).Underlying().(*types.Struct); !ok {
			return false
		}
		for _, element := range expression.Elts {
			if pair, ok := element.(*ast.KeyValueExpr); ok {
				element = pair.Value
			}
			if !httpReplayStable(fc, element) {
				return false
			}
		}
		return true
	}
	return false
}
