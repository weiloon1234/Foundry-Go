package generate

import (
	"fmt"
	"go/ast"
)

// Legacy handwritten model method names, retained for migration diagnostics.
const (
	relationSetMethod       = "DefineRelations"
	aggregateSetMethod      = "DefineAggregates"
	extensionSetMethod      = "DefineExtensions"
	globalScopeSourceMethod = "DefineGlobalScopes"
)

// Model declaration kinds for handwritten package functions.
const (
	kindRelations    = "Relations"
	kindAggregates   = "Aggregates"
	kindExtensions   = "Extensions"
	kindGlobalScopes = "GlobalScopes"
)

// modelDeclarationFunction returns the handwritten package function name for
// a model's declaration kind (e.g. "defineOrderRelations").
func modelDeclarationFunction(model string, kind string) string {
	return "define" + model + kind
}

// checkDeprecatedModelMethods reports an actionable migration diagnostic when
// a model declares any of the four old declaration methods in its method set.
func checkDeprecatedModelMethods(p *packageInput, m model) error {
	checks := []struct {
		method string
		kind   string
		ret    string
	}{
		{relationSetMethod, kindRelations, m.name + "RelationSet"},
		{aggregateSetMethod, kindAggregates, m.name + "AggregateSet"},
		{extensionSetMethod, kindExtensions, m.name + "ExtensionSet"},
		{globalScopeSourceMethod, kindGlobalScopes, "[]query.GlobalScope[" + m.name + "]"},
	}
	for _, c := range checks {
		if pos, found := p.methods[m.name][c.method]; found {
			fnName := modelDeclarationFunction(m.name, c.kind)
			return p.diagnostic(pos, fmt.Sprintf("model %s declares %s as a method; declare func %s() %s instead (methods on model types make every importing package compile the model's query engine)", m.name, c.method, fnName, c.ret))
		}
	}
	return nil
}

// validDeclarationFunction checks syntactically that a handwritten package
// function has no receiver, no type parameters, no parameters, and exactly one
// return matching resultType.
func validDeclarationFunction(p *packageInput, fnName string, resultType string) bool {
	for _, file := range p.files {
		for _, decl := range file.syntax.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != fnName {
				continue
			}
			if fn.Type.TypeParams != nil && fn.Type.TypeParams.NumFields() != 0 {
				return false
			}
			if fn.Type.Params != nil && fn.Type.Params.NumFields() != 0 {
				return false
			}
			if fn.Type.Results == nil || fn.Type.Results.NumFields() != 1 {
				return false
			}
			result, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
			return ok && result.Name == resultType
		}
	}
	return false
}
