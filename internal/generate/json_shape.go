package generate

import (
	"go/types"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

func jsonProperties(typ types.Type) ([]jsonshape.Property[types.Type], error) {
	return jsonshape.Collect(typ, jsonshape.Adapter[types.Type]{
		NumFields: func(t types.Type) (int, bool) {
			structure, ok := types.Unalias(t).Underlying().(*types.Struct)
			if !ok {
				return 0, false
			}
			return structure.NumFields(), true
		},
		Field: func(t types.Type, i int) jsonshape.Field[types.Type] {
			structure := types.Unalias(t).Underlying().(*types.Struct)
			f := structure.Field(i)
			return jsonshape.Field[types.Type]{Name: f.Name(), Type: types.Unalias(f.Type()), Tag: reflect.StructTag(structure.Tag(i)).Get("json"), Anonymous: f.Embedded(), Exported: f.Exported()}
		},
		Pointer: func(t types.Type) (types.Type, bool) {
			if p, ok := types.Unalias(t).(*types.Pointer); ok {
				return types.Unalias(p.Elem()), true
			}
			return types.Unalias(t), false
		},
		Optional: func(t types.Type) bool { _, ok := jsonWrapper(t, "Optional"); return ok },
		QuotedScalar: func(t types.Type) bool {
			basic, ok := types.Unalias(t).Underlying().(*types.Basic)
			return ok && basic.Info()&(types.IsBoolean|types.IsString|types.IsInteger|types.IsFloat) != 0
		},
	})
}

func jsonWrapper(t types.Type, name string) (types.Type, bool) {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if ok && isNamed(n, framework+"/value", name) && n.TypeArgs().Len() == 1 {
		return n.TypeArgs().At(0), true
	}
	return nil, false
}

func jsonPayload(t types.Type) types.Type {
	// Optional describes property presence. Once selected, the payload is T.
	for {
		if _, pointer := types.Unalias(t).(*types.Pointer); pointer {
			return t
		}
		inner, ok := jsonWrapper(t, "Optional")
		if !ok {
			return t
		}
		t = inner
	}
}

func jsonBase(t types.Type) types.Type {
	for {
		if p, ok := types.Unalias(t).(*types.Pointer); ok {
			t = p.Elem()
			continue
		}
		found := false
		for _, name := range []string{"Nullable", "Optional", "JSON"} {
			if inner, ok := jsonWrapper(t, name); ok {
				t = inner
				found = true
				break
			}
		}
		if !found {
			return types.Unalias(t)
		}
	}
}

func customJSONShape(t types.Type) bool {
	for _, candidate := range []types.Type{t, types.NewPointer(t)} {
		methods := types.NewMethodSet(candidate)
		for i := 0; i < methods.Len(); i++ {
			method := methods.At(i).Obj()
			if jsonCodecMethod(method.Name()) && jsonCodecSignature(method.Name(), method.Type().(*types.Signature)) {
				return true
			}
		}
	}
	return false
}

func jsonCodecMethod(name string) bool {
	switch name {
	case "MarshalJSON", "UnmarshalJSON", "MarshalText", "UnmarshalText", "MarshalJSONTo", "UnmarshalJSONFrom", "AppendText":
		return true
	}
	return false
}

func jsonCodecSignature(name string, sig *types.Signature) bool {
	bytes := types.NewSlice(types.Typ[types.Byte])
	errType := types.Universe.Lookup("error").Type()
	if sig.Variadic() {
		return false
	}
	switch name {
	case "MarshalJSON", "MarshalText":
		return sig.Params().Len() == 0 && sig.Results().Len() == 2 && types.Identical(sig.Results().At(0).Type(), bytes) && types.Identical(sig.Results().At(1).Type(), errType)
	case "AppendText":
		return sig.Params().Len() == 1 && types.Identical(sig.Params().At(0).Type(), bytes) && sig.Results().Len() == 2 && types.Identical(sig.Results().At(0).Type(), bytes) && types.Identical(sig.Results().At(1).Type(), errType)
	case "MarshalJSONTo", "UnmarshalJSONFrom":
		if sig.Params().Len() != 1 || sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), errType) {
			return false
		}
		pointer, ok := types.Unalias(sig.Params().At(0).Type()).(*types.Pointer)
		if !ok {
			return false
		}
		named, ok := types.Unalias(pointer.Elem()).(*types.Named)
		if !ok {
			return false
		}
		expected := "Encoder"
		if name == "UnmarshalJSONFrom" {
			expected = "Decoder"
		}
		return isNamed(named, "encoding/json/jsontext", expected)
	case "UnmarshalJSON", "UnmarshalText":
		return sig.Params().Len() == 1 && types.Identical(sig.Params().At(0).Type(), bytes) && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), errType)
	}
	return false
}
