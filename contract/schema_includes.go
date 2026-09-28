package contract

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// expandJSONTypes retains authored duplicate detection, while allowing a custom
// contract to share already-declared graph nodes. compileSchema normalizes and
// compares shared nodes before accepting them. Factory results contain resolved
// snapshots; including them does not re-execute their nested providers.
func expandJSONTypes(input []Type, shared bool) ([]Type, error) {
	if len(input) == 0 || len(input) > jsonwire.MaxNodes {
		return nil, invalidSchema()
	}
	seen := make(map[TypeID]bool, len(input))
	for _, typ := range input {
		if !declarationText(string(typ.ID)) || (!shared && seen[typ.ID]) {
			return nil, invalidSchema()
		}
		seen[typ.ID] = true
	}
	result := make([]Type, 0, len(input))
	for _, typ := range input {
		if len(result) >= jsonwire.MaxNodes {
			return nil, invalidSchema()
		}
		if typ.jsonContract == nil {
			result = append(result, typ)
			continue
		}
		// A provider placeholder has no independently authored shape.
		if typ.Kind != "" || typ.Nullable || typ.Discriminator != "" || len(typ.Variants) != 0 || len(typ.Properties) != 0 || typ.Key != nil || typ.jsonKey != nil ||
			typ.Element != "" || typ.Length.IsSet() || typ.Bits != 0 ||
			typ.Signed || typ.Format != "" || typ.byteElement != "" || len(typ.Cases) != 0 ||
			typ.enumCases != nil {
			return nil, invalidSchema()
		}
		var description Schema
		err := callback.Isolated("include custom JSON contract", func() error {
			var err error
			description, err = typ.jsonContract()
			return err
		})
		if err != nil {
			return nil, fault.Wrap(fault.Invalid, "invalid custom JSON contract", err)
		}
		if description.Root != typ.ID ||
			len(description.Types) > jsonwire.MaxNodes-len(result) {
			return nil, invalidSchema()
		}
		for _, included := range description.Types {
			if included.jsonContract != nil || included.enumCases != nil {
				return nil, invalidSchema()
			}
			result = append(result, included)
		}
	}
	return result, nil
}

func sameNormalizedType(left, right Type) bool {
	if !sameJSONKeyRuntime(left.jsonKey, right.jsonKey) {
		return false
	}
	left.jsonKey, right.jsonKey = nil, nil
	// Both values have resolved private callbacks and canonical field/case
	// ordering. nil and allocated-empty metadata lists are equivalent.
	if len(left.Properties) == 0 {
		left.Properties = nil
	}
	if len(right.Properties) == 0 {
		right.Properties = nil
	}
	if len(left.Cases) == 0 {
		left.Cases = nil
	}
	if len(right.Cases) == 0 {
		right.Cases = nil
	}
	if len(left.Variants) == 0 {
		left.Variants = nil
	}
	if len(right.Variants) == 0 {
		right.Variants = nil
	}
	return reflect.DeepEqual(left, right)
}
