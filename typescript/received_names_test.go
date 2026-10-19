package typescript

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

func TestReceivedNamesFollowRecursiveAndSharedSchemaEdges(t *testing.T) {
	key, err := contract.StringJSONKey[i18n.LocaleID]().Description()
	if err != nil {
		t.Fatal(err)
	}
	document := manifest.Document{Roots: []contract.TypeID{"Root", "CycleA"}, Types: []contract.Type{
		{ID: "Root", Kind: contract.UnionKind, Discriminator: "kind", Variants: []contract.Variant{{Tag: "record", Type: "Record"}}},
		{ID: "Record", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "values", Type: "Array"}, {Name: "locales", Type: "Locales"}, {Name: "empty_cycle", Type: "EmptyA"}}},
		{ID: "Array", Kind: contract.ArrayKind, Element: "Map"},
		{ID: "Map", Kind: contract.MapKind, Element: "Alias"},
		{ID: "Alias", Kind: contract.AliasKind, Element: "Quoted"},
		{ID: "Quoted", Kind: contract.QuotedKind, Element: "Enum"},
		{ID: "Enum", Kind: contract.IntegerKind, Bits: 32, Signed: true, Cases: []json.RawMessage{json.RawMessage(`1`)}},
		{ID: "Locales", Kind: contract.MapKind, Element: "String", Key: &key},
		{ID: "String", Kind: contract.StringKind},
		{ID: "CycleA", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "next", Type: "CycleB"}}},
		{ID: "CycleB", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "back", Type: "CycleA"}, {Name: "value", Type: "Enum"}}},
		{ID: "EmptyA", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "next", Type: "EmptyB"}}},
		{ID: "EmptyB", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "back", Type: "EmptyA"}}},
		{ID: "Unreachable", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "value", Type: "Enum"}}},
		{ID: "Taken", Kind: contract.StringKind},
	}}
	names := make(map[contract.TypeID]string)
	for _, typ := range document.Types {
		names[typ.ID] = string(typ.ID)
	}
	names["Taken"] = "RootReceived"
	received, enums := receivedNames(document, names, map[string]bool{"RootReceived_": true})
	want := map[contract.TypeID]string{
		"Root": "RootReceived__", "Record": "RecordReceived", "Array": "ArrayReceived",
		"Map": "MapReceived", "Alias": "AliasReceived", "Quoted": "QuotedReceived",
		"Enum": "EnumReceived", "Locales": "LocalesReceived", "CycleA": "CycleAReceived", "CycleB": "CycleBReceived",
	}
	if !reflect.DeepEqual(received, want) {
		t.Fatal("received variants", received)
	}
	wantEnums := map[contract.TypeID]bool{"Root": true, "Record": true, "Array": true, "Map": true, "Alias": true, "Quoted": true, "Enum": true, "CycleA": true, "CycleB": true, "Unreachable": true}
	if !reflect.DeepEqual(enums, wantEnums) {
		t.Fatal("enum-containing schemas", enums)
	}
}

func TestConcurrentRenderingReturnsOwnedModules(t *testing.T) {
	source := renderBenchmarkManifest(t, 8, 16, false)
	surfaces := renderBenchmarkSurfaces(2)
	want, err := renderModules(source, "api", surfaces)
	if err != nil {
		t.Fatal(err)
	}
	for range 8 {
		t.Run("owned", func(t *testing.T) {
			t.Parallel()
			for range 2 {
				files, err := renderModules(source, "api", surfaces)
				if err != nil {
					t.Fatal(err)
				}
				for name, data := range files {
					if !bytes.Equal(data, want[name]) {
						t.Fatal("rendered bytes changed", name)
					}
					data[0] = '!'
				}
			}
		})
	}
}
