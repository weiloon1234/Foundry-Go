package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

type WrappedNumberDTO struct {
	Data value.Optional[value.Nullable[map[string]any]] `json:"data,omitzero"`
}

func TestJSONDecodePreservesWrappedDynamicNumbers(t *testing.T) {
	root := dtoRoot[WrappedNumberDTO]()
	descriptor := DefineJSON[WrappedNumberDTO](Schema{Root: root, Types: []Type{
		{ID: root, Kind: ObjectKind, Properties: []Property{{Name: "data", Type: "nullable"}}},
		{ID: "nullable", Kind: AliasKind, Element: "map", Nullable: true},
		{ID: "map", Kind: MapKind, Element: "dynamic", Nullable: true},
		{ID: "dynamic", Kind: DynamicKind, Nullable: true},
	}})
	result, err := descriptor.Decode(context.Background(), []byte(`{"data":{"exact":9007199254740993,"nested":[1.2300e2]}}`), dtoLimits())
	if err != nil {
		t.Fatal(err)
	}
	nullable, _ := result.Data.Get()
	data, _ := nullable.Get()
	if got := data["exact"]; got != json.Number("9007199254740993") {
		t.Fatalf("wrapped DTO number changed: %T %v", got, got)
	}
	if got := data["nested"].([]any)[0]; got != json.Number("1.2300e2") {
		t.Fatalf("wrapped DTO number lexeme changed: %T %v", got, got)
	}
}
