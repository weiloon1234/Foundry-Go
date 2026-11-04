package contract_test

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func TestRequireObjectRoot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		root  contract.Type
		other []contract.Type
		valid bool
	}{
		{"object", contract.Type{ID: "root", Kind: contract.ObjectKind}, nil, true},
		{"alias", contract.Type{ID: "root", Kind: contract.AliasKind, Element: "object"}, []contract.Type{{ID: "object", Kind: contract.ObjectKind}}, false},
		{"nullable object", contract.Type{ID: "root", Kind: contract.ObjectKind, Nullable: true}, nil, false},
		{"nullable alias", contract.Type{ID: "root", Kind: contract.AliasKind, Element: "object", Nullable: true}, []contract.Type{{ID: "object", Kind: contract.ObjectKind}}, false},
		{"nullable target", contract.Type{ID: "root", Kind: contract.AliasKind, Element: "object"}, []contract.Type{{ID: "object", Kind: contract.ObjectKind, Nullable: true}}, false},
		{"missing target", contract.Type{ID: "root", Kind: contract.AliasKind, Element: "missing"}, nil, false},
		{"cycle", contract.Type{ID: "root", Kind: contract.AliasKind, Element: "root"}, nil, false},
		{"scalar", contract.Type{ID: "root", Kind: contract.StringKind}, nil, false},
		{"dynamic", contract.Type{ID: "root", Kind: contract.DynamicKind}, nil, false},
		{"array", contract.Type{ID: "root", Kind: contract.ArrayKind, Element: "object"}, []contract.Type{{ID: "object", Kind: contract.ObjectKind}}, false},
		{"map", contract.Type{ID: "root", Kind: contract.MapKind, Element: "object"}, []contract.Type{{ID: "object", Kind: contract.ObjectKind}}, false},
		{"union", contract.Type{ID: "root", Kind: contract.UnionKind}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := contract.RequireObjectRoot(contract.Schema{Root: tc.root.ID, Types: append([]contract.Type{tc.root}, tc.other...)})
			if (err == nil) != tc.valid {
				t.Fatal("object root acceptance", err)
			}
		})
	}
}

type ObjectRootDTO struct{ Name string }

func TestDefineJSONRefusesAliasRoot(t *testing.T) {
	root := contract.TypeID(reflect.TypeFor[ObjectRootDTO]().PkgPath() + ".ObjectRootDTO")
	schema := contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.AliasKind, Element: "object"}, {ID: "object", Kind: contract.ObjectKind}}}
	if err := contract.DefineJSON[ObjectRootDTO](schema).Validate(); err == nil {
		t.Fatal("alias root accepted")
	}
	schema.Types[0] = contract.Type{ID: root, Kind: contract.ObjectKind}
	if err := contract.DefineJSON[ObjectRootDTO](schema).Validate(); err != nil {
		t.Fatal("direct object refused", err)
	}
}

func TestJSONValidationSteps(t *testing.T) {
	root := contract.TypeID(reflect.TypeFor[ObjectRootDTO]().PkgPath() + ".ObjectRootDTO")
	descriptor := contract.DefineJSON[ObjectRootDTO](contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "Name", Type: "string", Required: true}}}, {ID: "string", Kind: contract.StringKind}}})
	limits := contract.JSONLimits{Bytes: 100, Depth: 1, Nodes: 3, Steps: 4, Issues: 1}
	if steps, err := descriptor.ValidationSteps(t.Context(), []byte(`{"Name":"A"}`), limits); err != nil || steps != 4 {
		t.Fatal(steps, err)
	}
	limits.Steps--
	if _, err := descriptor.ValidationSteps(t.Context(), []byte(`{"Name":"A"}`), limits); err == nil {
		t.Fatal("work bound ignored")
	}
}
