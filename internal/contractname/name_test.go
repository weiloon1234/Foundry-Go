package contractname

import (
	"regexp"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/value"
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

func TestModelIDNamesAndCollisions(t *testing.T) {
	// Single model ID resolves to <Model>ID.
	single, err := Schemas([]contract.Type{
		{ID: "github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/models.User]"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := single["github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/models.User]"]; got != "UserID" {
		t.Fatalf("single model ID name = %q, want %q", got, "UserID")
	}

	// Collision between app type UserID and model.ID[User] qualifies by package.
	appCollision, err := Schemas([]contract.Type{
		{ID: "foundry.test/consumer/types.UserID"},
		{ID: "github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/models.User]"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := appCollision["foundry.test/consumer/types.UserID"]; got != "Types_UserID" {
		t.Fatalf("types.UserID = %q, want Types_UserID", got)
	}
	if got := appCollision["github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/models.User]"]; got != "Models_UserID" {
		t.Fatalf("model.ID[models.User] = %q, want Models_UserID", got)
	}

	// Cross-package model.ID collision: genericdto.User vs models.User qualifies by model package.
	modelCollision, err := Schemas([]contract.Type{
		{ID: "github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/genericdto.User]"},
		{ID: "github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/models.User]"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := modelCollision["github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/genericdto.User]"]; got != "Genericdto_UserID" {
		t.Fatalf("genericdto.User = %q, want Genericdto_UserID", got)
	}
	if got := modelCollision["github.com/weiloon1234/Foundry-Go/model.ID[foundry.test/consumer/models.User]"]; got != "Models_UserID" {
		t.Fatalf("models.User = %q, want Models_UserID", got)
	}
}

func TestAnonymousStructNames(t *testing.T) {
	anonID := contract.TypeID("go:anon-profile-1234")
	parentID := contract.TypeID("foundry.test/consumer/admin.CreateAdminRequest")
	types := []contract.Type{
		{
			ID:   parentID,
			Kind: contract.ObjectKind,
			Properties: []contract.Property{
				{Name: "profile", Type: anonID},
			},
		},
		{
			ID:   anonID,
			Kind: contract.ObjectKind,
			Properties: []contract.Property{
				{Name: "bio", Type: "string"},
			},
		},
	}
	names, err := Schemas(types, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := names[parentID]; got != "CreateAdminRequest" {
		t.Fatalf("parent = %q, want CreateAdminRequest", got)
	}
	if got := names[anonID]; got != "CreateAdminRequestProfile" {
		t.Fatalf("anon = %q, want CreateAdminRequestProfile", got)
	}
}

func TestSliceOfAnonymousStruct(t *testing.T) {
	sliceID := contract.TypeID("go:slice-anon-1234")
	elemID := contract.TypeID("go:elem-anon-1234")
	reportID := contract.TypeID("example.com/reports.Report")

	types := []contract.Type{
		{
			ID:   reportID,
			Kind: contract.ObjectKind,
			Properties: []contract.Property{
				{Name: "items", Type: sliceID},
			},
		},
		{
			ID:      sliceID,
			Kind:    contract.ArrayKind,
			Element: elemID,
		},
		{
			ID:   elemID,
			Kind: contract.ObjectKind,
			Properties: []contract.Property{
				{Name: "count", Type: "int"},
			},
		},
	}
	names, err := Schemas(types, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := names[elemID]; got != "ReportItem" {
		t.Fatalf("anon elem = %q, want ReportItem", got)
	}
	if got := names[sliceID]; got != "List_ReportItem" {
		t.Fatalf("anon slice = %q, want List_ReportItem", got)
	}
}

func TestCollectionsAndReservedPrimitives(t *testing.T) {
	sliceID := contract.TypeID("go:slice-filter-1234")
	fixedArrayID := contract.TypeID("go:array-byte-1234")
	localeMapID := contract.TypeID("go:locale-map-1234")
	ordinaryMapID := contract.TypeID("go:map-invoice-1234")

	types := []contract.Type{
		{ID: "string"},
		{ID: "error"},
		{ID: "github.com/weiloon1234/Foundry-Go/datatable.Filter"},
		{ID: "example.com/billing.Invoice"},
		{
			ID:      sliceID,
			Kind:    contract.ArrayKind,
			Element: "github.com/weiloon1234/Foundry-Go/datatable.Filter",
		},
		{
			ID:      fixedArrayID,
			Kind:    contract.ArrayKind,
			Element: "byte",
			Length:  value.Set(16),
		},
		{
			ID:      localeMapID,
			Kind:    contract.MapKind,
			Element: "string",
			Key: &contract.JSONKeyInfo{
				Value: contract.Type{ID: "github.com/weiloon1234/Foundry-Go/i18n.LocaleID"},
			},
		},
		{
			ID:      ordinaryMapID,
			Kind:    contract.MapKind,
			Element: "example.com/billing.Invoice",
			Key: &contract.JSONKeyInfo{
				Value: contract.Type{ID: "string"},
			},
		},
	}
	// With reserved "String" and "Error":
	names, err := Schemas(types, func(name string) bool {
		return name == "String" || name == "Error"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := names["string"]; got != "GoString" {
		t.Fatalf("string = %q, want GoString", got)
	}
	if got := names["error"]; got != "GoError" {
		t.Fatalf("error = %q, want GoError", got)
	}
	if got := names[sliceID]; got != "List_Filter" {
		t.Fatalf("slice = %q, want List_Filter", got)
	}
	if got := names[fixedArrayID]; got != "Array_16_Byte" {
		t.Fatalf("fixed array = %q, want Array_16_Byte", got)
	}
	if got := names[localeMapID]; got != "LocaleMap_String" {
		t.Fatalf("locale map = %q, want LocaleMap_String", got)
	}
	if got := names[ordinaryMapID]; got != "Map_String_Invoice" {
		t.Fatalf("ordinary map = %q, want Map_String_Invoice", got)
	}
}

func TestMapSchemaNamesQualifyCollidingKeys(t *testing.T) {
	types := []contract.Type{
		{
			ID:      "go:map-a-code-string",
			Kind:    contract.MapKind,
			Element: "string",
			Key: &contract.JSONKeyInfo{
				Value: contract.Type{ID: "example.com/a.Code", Kind: contract.StringKind},
			},
		},
		{
			ID:      "go:map-b-code-string",
			Kind:    contract.MapKind,
			Element: "string",
			Key: &contract.JSONKeyInfo{
				Value: contract.Type{ID: "example.com/b.Code", Kind: contract.StringKind},
			},
		},
	}
	names, err := Schemas(types, nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[contract.TypeID]string{
		"go:map-a-code-string": "Map_A_Code_GoString",
		"go:map-b-code-string": "Map_B_Code_GoString",
	} {
		if got := names[id]; got != want {
			t.Errorf("name of %s = %q, want %q", id, got, want)
		}
	}
	t.Run("deeper key qualification", func(t *testing.T) {
		types[0].Key.Value.ID = "example.com/a/shared.Code"
		types[1].Key.Value.ID = "example.com/b/shared.Code"
		names, err := Schemas(types, nil)
		if err != nil {
			t.Fatal(err)
		}
		for id, want := range map[contract.TypeID]string{
			"go:map-a-code-string": "Map_A_Shared_Code_String",
			"go:map-b-code-string": "Map_B_Shared_Code_String",
		} {
			if got := names[id]; got != want {
				t.Errorf("name of %s = %q, want %q", id, got, want)
			}
		}
	})
}

func TestSchemaNamesExhaustReservedReadableCandidates(t *testing.T) {
	for _, typ := range []contract.Type{
		{ID: "go:list-string", Kind: contract.ArrayKind, Element: "string"},
		{ID: "go:array-string", Kind: contract.ArrayKind, Element: "string", Length: value.Set(2)},
		{ID: "go:map-string", Kind: contract.MapKind, Element: "string"},
		{
			ID: "go:map-code", Kind: contract.MapKind, Element: "string",
			Key: &contract.JSONKeyInfo{Value: contract.Type{ID: "example.com/keys.Code"}},
		},
		{
			ID: "go:locale-map", Kind: contract.MapKind, Element: "string",
			Key: &contract.JSONKeyInfo{Value: contract.Type{ID: "example.com/i18n.LocaleID"}},
		},
		{ID: "go:nullable-string", Kind: contract.AliasKind, Element: "string", Nullable: true},
		{ID: "go:alias-string", Kind: contract.AliasKind, Element: "string"},
		{ID: "go:child-object", Kind: contract.ObjectKind},
	} {
		t.Run(string(typ.ID), func(t *testing.T) {
			types := []contract.Type{
				typ,
				{
					ID: "example.com/owner.Parent", Kind: contract.ObjectKind,
					Properties: []contract.Property{{Name: "child", Type: typ.ID}},
				},
			}
			want := Symbol(string(typ.ID))
			names, err := Schemas(types, func(name string) bool {
				return name != want && name != "Parent"
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := names[typ.ID]; got != want {
				t.Fatalf("name = %q, want identity digest %q", got, want)
			}
		})
	}
}
