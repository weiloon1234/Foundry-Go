package contract_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func TestSerializedMapMetadataRetainsNativeCodecBoundary(t *testing.T) {
	mapType := contract.JSONMapType[map[int64]string]("map", "string", "int64", contract.IntegerJSONKey[int64]())
	source := contract.Schema{Root: "map", Types: []contract.Type{mapType, {ID: "string", Kind: contract.StringKind}}}
	if err := contract.DefineJSONField[map[int64]string](source).Validate(); err != nil {
		t.Fatal("native binding is invalid", err)
	}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var restored contract.Schema
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	metadata, err := restored.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Types[0].Key == nil || metadata.Types[0].Key.Value.Bits != 64 {
		t.Fatal("map-key identity or width lost")
	}
	if contract.DefineJSONField[map[int64]string](metadata).Validate() == nil {
		t.Fatal("serialized metadata acquired a native key codec")
	}
	again, err := metadata.Normalize()
	if err != nil || !reflect.DeepEqual(again, metadata) {
		t.Fatal("normalization is not idempotent", err)
	}
	metadata.Types[0].Key.Value.ID = "changed"
	if restored.Types[0].Key.Value.ID == "changed" {
		t.Fatal("metadata aliases input")
	}
}

func TestMetadataNormalizationSharesGraphFailures(t *testing.T) {
	for _, types := range [][]contract.Type{
		{{ID: "root", Kind: contract.AliasKind, Element: "missing"}},
		{{ID: "root", Kind: contract.AliasKind, Element: "root"}},
		{{ID: "root", Kind: contract.StringKind}, {ID: "root", Kind: contract.IntegerKind}},
		{{ID: "root", Kind: contract.MapKind, Element: "value", Key: &contract.JSONKeyInfo{Value: contract.Type{ID: "key", Kind: contract.StringKind}, Syntax: contract.CustomJSONKeySyntax}}, {ID: "value", Kind: contract.StringKind}},
	} {
		if _, err := (contract.Schema{Root: "root", Types: types}).Normalize(); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}
