package contractname

import (
	"regexp"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func TestSchemaNamesAreStableAndKeepFullIdentity(t *testing.T) {
	valid := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	for _, id := range []string{"", "123", "type", "pkg.Model[other.User]", "pkg/a.User", "pkg/b.User", "x. \""} {
		if !valid.MatchString(Symbol(id)) {
			t.Fatal("invalid target name", id)
		}
	}
	first, err := Schemas([]contract.Type{{ID: "pkg/a.User"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Schemas([]contract.Type{{ID: "pkg/b.User"}, {ID: "pkg/a.User"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first["pkg/a.User"] != "User" || second["pkg/a.User"] != "A_User" || second["pkg/b.User"] != "B_User" {
		t.Fatalf("collision qualification = %v, %v", first, second)
	}
	again, err := Schemas([]contract.Type{{ID: "pkg/a.User"}, {ID: "pkg/b.User"}}, nil)
	if err != nil || again["pkg/a.User"] != second["pkg/a.User"] {
		t.Fatal("names depend on catalogue order", again, err)
	}
}

func TestSchemaNamesAreReadableAndSurvivePackageMoves(t *testing.T) {
	valid := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	ids := []contract.TypeID{
		"example.com/app/billing.Invoice",
		"example.com/app/api.Page[example.com/app/billing.Invoice]",
		"[]example.com/app/billing.Invoice",
		"map[string]example.com/app/billing.Invoice",
		"quoted:int64",
		"nonnull:[]string",
		"string",
		"go:0123abcd",
		`example.com/app.Box[struct{Label string "json:\"a,b\""}]`,
		"example.com/app.Reserved",
		"example.com/app/one/shared.Item",
		"example.com/app/two/shared.Item",
	}
	var types []contract.Type
	for _, id := range ids {
		types = append(types, contract.Type{ID: id})
	}
	names, err := Schemas(types, func(name string) bool { return name == "Reserved" })
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[contract.TypeID]string{
		"example.com/app/billing.Invoice":                           "Invoice",
		"example.com/app/api.Page[example.com/app/billing.Invoice]": "Page_Invoice",
		"[]example.com/app/billing.Invoice":                         "List_Invoice",
		"map[string]example.com/app/billing.Invoice":                "Map_String_Invoice",
		"quoted:int64":     "Quoted_Int64",
		"nonnull:[]string": "Nonnull_List_String",
		`example.com/app.Box[struct{Label string "json:\"a,b\""}]`: "Box_Struct_Label_String",
		"example.com/app.Reserved":                                 "App_Reserved",
		"example.com/app/one/shared.Item":                          "One_Shared_Item",
		"example.com/app/two/shared.Item":                          "Two_Shared_Item",
	} {
		if names[id] != want {
			t.Errorf("name of %s = %q, want %q", id, names[id], want)
		}
	}
	if names["go:0123abcd"] != Symbol("go:0123abcd") {
		t.Error("anonymous identity did not use its digest name", names["go:0123abcd"])
	}
	seen := make(map[string]bool)
	for id, name := range names {
		if !valid.MatchString(name) || seen[name] {
			t.Fatalf("invalid or duplicate name %q for %s", name, id)
		}
		seen[name] = true
	}
	moved, err := Schemas([]contract.Type{{ID: "example.com/other/place.Invoice"}}, nil)
	if err != nil || moved["example.com/other/place.Invoice"] != "Invoice" {
		t.Fatal("a moved package changed its type name", moved, err)
	}
}
