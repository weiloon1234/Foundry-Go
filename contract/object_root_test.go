package contract_test

import (
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
		{"alias", contract.Type{ID: "root", Kind: contract.AliasKind, Element: "object"}, []contract.Type{{ID: "object", Kind: contract.ObjectKind}}, true},
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
