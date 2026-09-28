package contract

import (
	"encoding/json"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// EnumType uses an enum's existing typed descriptor as the source of its wire
// cases. Generated DTO declarations use this for local and imported enums.
// DefineJSON resolves and validates cases once, containing codec failures and
// retaining only owned normalized metadata. The returned Type is a declaration;
// use a validated JSON descriptor's Description for client export.
func EnumType[E enum.Scalar](id TypeID, descriptor enum.Descriptor[E]) Type {
	typ := Type{ID: id}
	goType := reflect.TypeFor[E]()
	switch goType.Kind() {
	case reflect.String:
		typ.Kind = StringKind
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		typ.Kind, typ.Bits, typ.Signed = IntegerKind, uint8(goType.Bits()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		typ.Kind, typ.Bits = IntegerKind, uint8(goType.Bits())
	}
	typ.enumCases = func() ([]json.RawMessage, error) {
		definition, err := descriptor.Definition()
		if err != nil {
			return nil, err
		}
		if len(definition.Cases) > jsonwire.MaxNodes {
			return nil, invalidSchema()
		}
		cases := make([]json.RawMessage, len(definition.Cases))
		for i, entry := range definition.Cases {
			cases[i] = entry.Value
		}
		return cases, nil
	}
	return typ
}
