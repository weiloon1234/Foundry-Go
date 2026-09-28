package generate

import (
	"fmt"
	"go/token"
	"go/types"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

type dtoMapKey struct {
	typ    types.Type
	kind   string
	owner  types.Type
	format contract.Format
}

func resolveDTOMapKey(p *packageInput, typ types.Type, pos token.Pos, models map[*types.Named]bool) (dtoMapKey, error) {
	typ = types.Unalias(typ)
	fail := func(message string) (dtoMapKey, error) { return dtoMapKey{}, p.diagnostic(pos, message) }
	key := dtoMapKey{typ: typ}
	if _, parameter := typ.(*types.TypeParam); parameter {
		key.kind = "parameter"
		return key, nil
	}
	if _, pointer := typ.(*types.Pointer); pointer {
		return fail("DTO map keys require concrete value identities")
	}
	if named, ok := typ.(*types.Named); ok {
		if models[named] || hasDTOModelIdentity(named) {
			return fail("persistence models cannot become DTO map keys; use a typed model ID")
		}
		if explicit, diagnostic := customDTOKeyContract(named); diagnostic != "" {
			return fail(diagnostic)
		} else if explicit {
			key.kind = "custom"
			return key, nil
		}
		if isNamed(named, framework+"/model", "ID") && named.TypeArgs().Len() == 1 {
			key.kind, key.owner = "model", named.TypeArgs().At(0)
			return key, nil
		}
		if p.enumTypes[named] || hasEnumDescriptor(named) {
			key.kind = "enum"
			return key, nil
		}
		if format := dtoScalarFormat(named); format != "" {
			key.kind, key.format = "formatted", format
			return key, nil
		}
	}
	basic, ok := typ.Underlying().(*types.Basic)
	if !ok {
		return fail("custom DTO map keys require a typed JSONKeyContract value method")
	}
	for _, candidate := range []types.Type{typ, types.NewPointer(typ)} {
		methods := types.NewMethodSet(candidate)
		for i := 0; i < methods.Len(); i++ {
			method := methods.At(i).Obj()
			if jsonshape.MapKeyCodecMethod(method.Name(), basic.Kind() == types.String) &&
				jsonCodecSignature(method.Name(), method.Type().(*types.Signature)) {
				return fail("custom DTO map key codecs require a typed JSONKeyContract value method")
			}
		}
	}
	switch basic.Kind() {
	case types.String:
		key.kind = "string"
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		key.kind = "integer"
	default:
		return fail("unsupported DTO map key; use a string, integer, enum, model ID or declared native text key")
	}
	return key, nil
}

func customDTOKeyContract(typ *types.Named) (bool, string) {
	methods := types.NewMethodSet(types.NewPointer(typ))
	for i := 0; i < methods.Len(); i++ {
		method := methods.At(i).Obj()
		if method.Name() != "JSONKeyContract" {
			continue
		}
		if !typ.Obj().Exported() {
			return false, "JSONKeyContract values must be exported named types"
		}
		signature, ok := method.Type().(*types.Signature)
		if !ok || signature.Variadic() || signature.Params().Len() != 0 || signature.Results().Len() != 1 {
			return false, "JSONKeyContract must be a value method returning contract.JSONKey of the same type"
		}
		if _, pointer := signature.Recv().Type().(*types.Pointer); pointer {
			return false, "JSONKeyContract must be a value method independent of receiver state"
		}
		result, ok := types.Unalias(signature.Results().At(0).Type()).(*types.Named)
		if !ok || !isNamed(result, framework+"/contract", "JSONKey") || result.TypeArgs().Len() != 1 ||
			!types.Identical(result.TypeArgs().At(0), typ) {
			return false, "JSONKeyContract must return contract.JSONKey of the same concrete key type"
		}
		return true, ""
	}
	return false, ""
}

func (e *emitter) dtoMapKeyConstructor(key dtoMapKey) string {
	pkg := e.useNamed(framework+"/contract", "foundrycontract")
	typ := e.typeName(key.typ)
	switch key.kind {
	case "parameter":
		if name := e.genericKeys[key.typ.(*types.TypeParam)]; name != "" {
			return name
		}
		e.err = fmt.Errorf("generic DTO map key has no typed key argument")
		return ""
	case "string":
		return fmt.Sprintf("%s.StringJSONKey[%s]()", pkg, typ)
	case "integer":
		return fmt.Sprintf("%s.IntegerJSONKey[%s]()", pkg, typ)
	case "model":
		return fmt.Sprintf("%s.ModelIDJSONKey[%s]()", pkg, e.typeName(key.owner))
	case "enum":
		return fmt.Sprintf("%s.ResolveJSONKey[%s](func() %s.JSONKey[%s] { return %s.EnumJSONKey((*new(%s)).EnumDescriptor()) })", pkg, typ, pkg, typ, pkg, typ)
	case "formatted":
		return fmt.Sprintf("%s.DefineJSONKey(%s.DefineScalar[%s](%s.Type{ID:%q,Kind:%s.StringKind,Format:%s.Format(%q)}))", pkg, pkg, typ, pkg, dtoTypeID(key.typ), pkg, pkg, key.format)
	case "custom":
		return fmt.Sprintf("%s.ResolveJSONKey[%s]((*new(%s)).JSONKeyContract)", pkg, typ, typ)
	}
	e.err = fmt.Errorf("DTO map key has no resolved codec")
	return ""
}
