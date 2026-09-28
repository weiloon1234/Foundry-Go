package clientcontracts

import "github.com/weiloon1234/Foundry-Go/contract"

// ExplicitLabels exercises the public metadata boundary: arbitrary string keys
// retain a concrete string value contract, without a constrained key descriptor.
type ExplicitLabels map[string]string

func ExplicitLabelsJSON() (contract.JSON[ExplicitLabels], error) {
	schema, err := contract.StringJSON[string]().Description()
	if err != nil {
		return contract.JSON[ExplicitLabels]{}, err
	}
	const id contract.TypeID = "foundry.test/consumer/clientcontracts.ExplicitLabels"
	schema.Types = append(schema.Types, contract.Type{ID: id, Kind: contract.MapKind, Element: schema.Root, Nullable: true})
	schema.Root = id
	codec := contract.DefineJSONField[ExplicitLabels](schema)
	return codec, codec.Validate()
}
