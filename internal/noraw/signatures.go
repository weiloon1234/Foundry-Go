package noraw

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"go/types"
)

var rawPackages = []string{databasePath, databasePath + "/orm", databasePath + "/orm/rows", databasePath + "/orm/expr"}

type rawSignature struct {
	symbol    string
	signature *types.Signature
}

// Read declarations from the project's framework version. Generic parameters
// remain patterns, so a caller does not need to mention a raw constructor first.
func (g *packageGraph) rawSignatures() ([]rawSignature, types.Type, error) {
	var result []rawSignature
	var executor types.Type
	addSignature := func(symbol string, sig *types.Signature) {
		for _, prior := range result {
			if types.Identical(prior.signature, sig) {
				return
			}
		}
		result = append(result, rawSignature{symbol, sig})
	}
	add := func(f *types.Func) {
		symbol := frameworkRaw(f)
		if symbol == "" {
			return
		}
		sig := f.Type().(*types.Signature)
		addSignature(symbol, sig)
		if sig.Recv() != nil {
			params := []*types.Var{sig.Recv()}
			for i := 0; i < sig.Params().Len(); i++ {
				params = append(params, sig.Params().At(i))
			}
			addSignature(symbol, types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), sig.Results(), sig.Variadic()))
		}
	}
	for _, path := range rawPackages {
		p, err := g.importer.Import(path)
		if err != nil {
			return nil, nil, safeerror.New(fmt.Sprintf("no-raw: cannot load raw API type information"))
		}
		if path == databasePath {
			executor = p.Scope().Lookup("Executor").Type()
		}
		for _, name := range p.Scope().Names() {
			switch obj := p.Scope().Lookup(name).(type) {
			case *types.Func:
				add(obj)
			case *types.TypeName:
				if _, ok := types.Unalias(obj.Type()).(*types.Named); !ok {
					continue
				}
				for _, typ := range []types.Type{obj.Type(), types.NewPointer(obj.Type())} {
					set := types.NewMethodSet(typ)
					for i := 0; i < set.Len(); i++ {
						add(set.At(i).Obj().(*types.Func))
					}
				}
			}
		}
	}
	return result, executor, nil
}

func (r rawSignature) matches(candidate *types.Signature) bool {
	bindings := map[*types.TypeParam]types.Type{}
	if !matchSignature(r.signature, candidate, bindings) {
		return false
	}
	for param, value := range bindings {
		if !types.Satisfies(value, param.Constraint().Underlying().(*types.Interface)) {
			return false
		}
	}
	return true
}

// Compare the complete function signature, ignoring the method receiver because
// a bound method value has no receiver parameter. Only declared type parameters
// may vary, and repeated occurrences must resolve to the identical concrete type.
func matchSignature(pattern, value *types.Signature, bindings map[*types.TypeParam]types.Type) bool {
	return pattern.Variadic() == value.Variadic() && matchTuple(pattern.Params(), value.Params(), bindings) && matchTuple(pattern.Results(), value.Results(), bindings)
}
func matchTuple(pattern, value *types.Tuple, bindings map[*types.TypeParam]types.Type) bool {
	if pattern.Len() != value.Len() {
		return false
	}
	for i := 0; i < pattern.Len(); i++ {
		if !matchType(pattern.At(i).Type(), value.At(i).Type(), bindings) {
			return false
		}
	}
	return true
}
func matchType(pattern, value types.Type, bindings map[*types.TypeParam]types.Type) bool {
	pattern, value = types.Unalias(pattern), types.Unalias(value)
	if p, ok := pattern.(*types.TypeParam); ok {
		if prior, exists := bindings[p]; exists {
			return types.Identical(prior, value)
		}
		bindings[p] = value
		return true
	}
	switch p := pattern.(type) {
	case *types.Named:
		v, ok := value.(*types.Named)
		if !ok || !types.Identical(p.Origin(), v.Origin()) || p.TypeArgs().Len() != v.TypeArgs().Len() {
			return false
		}
		for i := 0; i < p.TypeArgs().Len(); i++ {
			if !matchType(p.TypeArgs().At(i), v.TypeArgs().At(i), bindings) {
				return false
			}
		}
		return true
	case *types.Slice:
		v, ok := value.(*types.Slice)
		return ok && matchType(p.Elem(), v.Elem(), bindings)
	case *types.Signature:
		v, ok := value.(*types.Signature)
		return ok && matchSignature(p, v, bindings)
	case *types.Pointer:
		v, ok := value.(*types.Pointer)
		return ok && matchType(p.Elem(), v.Elem(), bindings)
	default:
		return types.Identical(pattern, value)
	}
}
