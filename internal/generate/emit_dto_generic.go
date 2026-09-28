package generate

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func genericParameters(typ types.Type) *types.TypeParamList {
	if named, ok := types.Unalias(typ).(*types.Named); ok {
		return named.TypeParams()
	}
	return nil
}

func (e *emitter) genericDeclaration(typ types.Type) (string, string) {
	parameters := genericParameters(typ)
	if parameters.Len() == 0 {
		return "", ""
	}
	declarations, arguments := make([]string, parameters.Len()), make([]string, parameters.Len())
	for i := range parameters.Len() {
		p := parameters.At(i)
		arguments[i] = p.Obj().Name()
		declarations[i] = p.Obj().Name() + " " + e.typeName(p.Constraint())
	}
	return "[" + strings.Join(declarations, ",") + "]", "[" + strings.Join(arguments, ",") + "]"
}

func (e *emitter) genericArguments(typ types.Type, graph dtoGraph) (string, map[*types.TypeParam]string) {
	parameters := genericParameters(typ)
	names := make(map[*types.TypeParam]string, parameters.Len())
	declarations := make([]string, 0, parameters.Len())
	keys := make(map[*types.TypeParam]bool)
	e.genericKeys = make(map[*types.TypeParam]string)
	for _, node := range graph.nodes {
		if node.key != nil {
			if p, ok := node.key.typ.(*types.TypeParam); ok {
				keys[p] = true
			}
		}
	}
	pkg := e.useNamed(framework+"/contract", "foundrycontract")
	for i := range parameters.Len() {
		parameter := parameters.At(i)
		name := e.localName(fmt.Sprintf("foundryType%d", i))
		names[parameter] = name
		e.reserved[name] = true
		declarations = append(declarations, name+" "+pkg+".JSON["+parameter.Obj().Name()+"]")
		if keys[parameter] {
			key := e.localName(fmt.Sprintf("foundryKey%d", i))
			e.reserved[key] = true
			e.genericKeys[parameter] = key
			declarations = append(declarations, key+" "+pkg+".JSONKey["+parameter.Obj().Name()+"]")
		}
	}
	return strings.Join(declarations, ","), names
}

// genericSourceExpression emits a Go type expression from actual type nodes.
// Parameter substitutions cannot accidentally rewrite field names or tag text.
func (e *emitter) genericSourceExpression(typ types.Type, parameters map[*types.TypeParam]string) string {
	typ = types.Unalias(typ)
	quote := strconv.Quote
	join := func(parts ...string) string { return strings.Join(parts, "+") }
	switch t := typ.(type) {
	case *types.TypeParam:
		name, ok := parameters[t]
		if !ok {
			e.err = fmt.Errorf("generic DTO contains an unbound type parameter %s", t.Obj().Name())
			return `""`
		}
		return name + ".GoTypeName()"
	case *types.Named:
		base := t.Obj().Name()
		if t.Obj().Pkg() != nil {
			base = t.Obj().Pkg().Path() + "." + base
		}
		if t.TypeArgs().Len() == 0 {
			return quote(base)
		}
		parts := []string{quote(base + "[")}
		for i := range t.TypeArgs().Len() {
			if i != 0 {
				parts = append(parts, quote(", "))
			}
			parts = append(parts, e.genericSourceExpression(t.TypeArgs().At(i), parameters))
		}
		return join(append(parts, quote("]"))...)
	case *types.Pointer:
		return join(quote("*"), e.genericSourceExpression(t.Elem(), parameters))
	case *types.Slice:
		return join(quote("[]"), e.genericSourceExpression(t.Elem(), parameters))
	case *types.Array:
		return join(quote(fmt.Sprintf("[%d]", t.Len())), e.genericSourceExpression(t.Elem(), parameters))
	case *types.Map:
		return join(quote("map["), e.genericSourceExpression(t.Key(), parameters), quote("]"), e.genericSourceExpression(t.Elem(), parameters))
	case *types.Struct:
		parts := []string{quote("struct{")}
		for i := 0; i < t.NumFields(); i++ {
			field := t.Field(i)
			if i != 0 {
				parts = append(parts, quote("; "))
			}
			if !field.Embedded() {
				parts = append(parts, quote(field.Name()+" "))
			}
			parts = append(parts, e.genericSourceExpression(field.Type(), parameters))
			if t.Tag(i) != "" {
				parts = append(parts, quote(" "+quote(t.Tag(i))))
			}
		}
		return join(append(parts, quote("}"))...)
	default:
		return quote(types.TypeString(typ, func(p *types.Package) string { return p.Path() }))
	}
}

func hasTypeParameter(typ types.Type) bool {
	typ = types.Unalias(typ)
	switch t := typ.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for i := range t.TypeArgs().Len() {
			if hasTypeParameter(t.TypeArgs().At(i)) {
				return true
			}
		}
	case *types.Pointer:
		return hasTypeParameter(t.Elem())
	case *types.Slice:
		return hasTypeParameter(t.Elem())
	case *types.Array:
		return hasTypeParameter(t.Elem())
	case *types.Map:
		return hasTypeParameter(t.Key()) || hasTypeParameter(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if hasTypeParameter(t.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

func (e *emitter) genericIDExpressions(graph dtoGraph, parameters map[*types.TypeParam]string) map[contract.TypeID]string {
	result := make(map[contract.TypeID]string)
	pkg := e.useNamed(framework+"/contract", "foundrycontract")
	for _, node := range graph.nodes {
		if node.typ == nil || !hasTypeParameter(node.typ) {
			continue
		}
		named := false
		switch types.Unalias(node.typ).(type) {
		case *types.Named, *types.Basic:
			named = true
		}
		if parameter, ok := types.Unalias(node.typ).(*types.TypeParam); ok {
			result[node.wire.ID] = pkg + ".JSONArgumentID(" + parameters[parameter] + ")"
		} else {
			result[node.wire.ID] = fmt.Sprintf("%s.GoTypeID(%s,%t)", pkg, e.genericSourceExpression(node.typ, parameters), named)
		}
	}
	// Quoted scalar wrappers have synthetic nodes without a native Go type.
	for _, node := range graph.nodes {
		if node.typ == nil && node.wire.Kind == contract.QuotedKind {
			if base, ok := result[node.wire.Element]; ok {
				result[node.wire.ID] = fmt.Sprintf("%s.TypeID(%q+string(%s))", pkg, "quoted:", base)
			}
		}
	}
	return result
}
