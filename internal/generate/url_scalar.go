package generate

import (
	"fmt"
	"go/token"
	"go/types"
)

// URL scalar discovery is shared by path and query declarations. Cardinality,
// name grammar and escaping remain owned by their respective transport boundary.
type urlScalar struct {
	kind  string
	owner types.Type
}

func resolveURLScalar(p *packageInput, typ types.Type, position token.Pos, boundary string) (urlScalar, error) {
	base := types.Unalias(typ)
	fail := func(message string) (urlScalar, error) { return urlScalar{}, p.diagnostic(position, message) }
	if _, pointer := base.(*types.Pointer); pointer {
		return fail(boundary + " fields require concrete values, not pointers")
	}
	if named, ok := base.(*types.Named); ok {
		if isNamed(named, framework+"/model", "ID") && named.TypeArgs().Len() == 1 {
			return urlScalar{kind: "model", owner: named.TypeArgs().At(0)}, nil
		}
		if p.enumTypes[named] || hasEnumDescriptor(named) {
			// Same-package methods arrive in this batch; imported enums retain
			// their existing concrete descriptor and membership metadata.
			return urlScalar{kind: "enum", owner: named}, nil
		}
	}
	marshal, unmarshal, invalid := urlTextMethods(base)
	if invalid {
		return fail(boundary + " text methods must implement encoding.TextMarshaler and encoding.TextUnmarshaler signatures")
	}
	if marshal || unmarshal {
		if !marshal || !unmarshal {
			return fail(boundary + " text codec requires both MarshalText and UnmarshalText")
		}
		return urlScalar{kind: "text"}, nil
	}
	if basic, ok := base.Underlying().(*types.Basic); ok {
		switch basic.Kind() {
		case types.String:
			return urlScalar{kind: "string"}, nil
		case types.Float32, types.Float64:
			return urlScalar{kind: "float"}, nil
		case types.Bool:
			return urlScalar{kind: "bool"}, nil
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64, types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
			return urlScalar{kind: "integer"}, nil
		}
	}
	return fail("unsupported " + boundary + " field; use model.ID, a named scalar or a complete text codec")
}

func urlTextMethods(typ types.Type) (marshal, unmarshal, invalid bool) {
	methods := types.NewMethodSet(types.NewPointer(typ))
	for i := 0; i < methods.Len(); i++ {
		method := methods.At(i).Obj()
		if method.Name() != "MarshalText" && method.Name() != "UnmarshalText" {
			continue
		}
		signature, ok := method.Type().(*types.Signature)
		if !ok || !jsonCodecSignature(method.Name(), signature) {
			invalid = true
			continue
		}
		if method.Name() == "MarshalText" {
			marshal = true
		} else {
			unmarshal = true
		}
	}
	return
}

// Generated source identities unify URL, JSON value and map-key metadata.
// Undescribed custom text codecs stay explicit; source names do not infer shapes.
func (e *emitter) urlScalarCodec(http, boundary string, typ types.Type, scalar urlScalar) (string, error) {
	codec, err := e.urlValueCodec(http, boundary, typ, scalar)
	if err != nil || scalar.kind == "text" {
		return codec, err
	}
	return fmt.Sprintf("%s.URLType(%q,%s)", http, dtoTypeID(typ), codec), nil
}

func (e *emitter) urlValueCodec(http, boundary string, typ types.Type, scalar urlScalar) (string, error) {
	name := e.typeName(typ)
	switch scalar.kind {
	case "model":
		return fmt.Sprintf("%s.ModelID%s[%s]()", http, boundary, e.typeName(scalar.owner)), nil
	case "string":
		return fmt.Sprintf("%s.String%s[%s]()", http, boundary, name), nil
	case "integer":
		return fmt.Sprintf("%s.Integer%s[%s]()", http, boundary, name), nil
	case "float":
		return fmt.Sprintf("%s.Float%s[%s]()", http, boundary, name), nil
	case "bool":
		return fmt.Sprintf("%s.Bool%s[%s]()", http, boundary, name), nil
	case "enum":
		zero := "0"
		if types.Unalias(typ).Underlying().(*types.Basic).Kind() == types.String {
			zero = `""`
		}
		return fmt.Sprintf("%s.Enum%s[%s,*%s](%s(%s).EnumDescriptor())", http, boundary, name, name, name, zero), nil
	case "text":
		return fmt.Sprintf("%s.Text%s[%s,*%s]()", http, boundary, name, name), nil
	}
	return "", fmt.Errorf("URL field has no resolved codec")
}
