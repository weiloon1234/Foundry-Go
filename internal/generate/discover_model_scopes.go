package generate

import "go/types"

// discoverGlobalScopeSource validates a handwritten define<Model>GlobalScopes
// before anything is emitted, so an invalid signature is reported against the
// function instead of producing uncompilable generated code.
func discoverGlobalScopeSource(p *packageInput, m model) error {
	fnName := modelDeclarationFunction(m.name, kindGlobalScopes)
	declared, found := p.definedAt[fnName]
	if !found {
		return nil
	}
	diagnostic := "function " + fnName + " requires signature func " + fnName + "() []query.GlobalScope[" + m.name + "]"
	obj := p.types.Scope().Lookup(fnName)
	if obj == nil {
		return p.diagnostic(declared, diagnostic)
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return p.diagnostic(declared, diagnostic)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() != 0 || sig.Results().Len() != 1 || !globalScopeSlice(sig.Results().At(0).Type(), m.typ) {
		return p.diagnostic(fn.Pos(), diagnostic)
	}
	return nil
}

// globalScopeSlice accepts exactly []query.GlobalScope[M] (or an alias of it):
// the generated method value must be assignable to func() []GlobalScope[M].
func globalScopeSlice(typ types.Type, model *types.Named) bool {
	slice, ok := types.Unalias(typ).(*types.Slice)
	if !ok {
		return false
	}
	scope, ok := types.Unalias(slice.Elem()).(*types.Named)
	return ok && isNamed(scope, framework+"/database/query", "GlobalScope") && scope.TypeArgs().Len() == 1 && types.Identical(scope.TypeArgs().At(0), model)
}
