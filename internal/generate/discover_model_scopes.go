package generate

import "go/types"

// globalScopeSourceMethod is the handwritten model method declaring the model's
// global scopes. The generated query passes it, uncalled, as a lazy scope source.
const globalScopeSourceMethod = "DefineGlobalScopes"

// discoverGlobalScopeSource validates a handwritten DefineGlobalScopes before
// anything is emitted, so a pointer receiver or another signature is reported
// against the method instead of producing uncompilable generated code.
func discoverGlobalScopeSource(p *packageInput, m model) error {
	declared, found := p.methods[m.name][globalScopeSourceMethod]
	if !found {
		return nil
	}
	diagnostic := "model " + globalScopeSourceMethod + " requires a value receiver and signature func (" + m.name + ") " + globalScopeSourceMethod + "() []query.GlobalScope[" + m.name + "]"
	for i := range m.typ.NumMethods() {
		method := m.typ.Method(i)
		if method.Name() != globalScopeSourceMethod {
			continue
		}
		sig := method.Type().(*types.Signature)
		_, pointer := sig.Recv().Type().(*types.Pointer)
		if pointer || sig.Variadic() || sig.Params().Len() != 0 || sig.Results().Len() != 1 || !globalScopeSlice(sig.Results().At(0).Type(), m.typ) {
			return p.diagnostic(method.Pos(), diagnostic)
		}
		return nil
	}
	// Recorded syntactically but absent from the model's type information.
	return p.diagnostic(declared, diagnostic)
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
