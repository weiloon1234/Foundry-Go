package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestJSONPathNamesSeparateSegments(t *testing.T) {
	for _, pair := range [][2]string{{"A_B", "A__B"}, {"_", "__"}, {"Name", "Name"}} {
		if got := jsonPathName(pair[0]); got != pair[1] {
			t.Fatalf("%q: %q", pair[0], got)
		}
	}
	if jsonPathName("A_B") == jsonPathName("A")+"_"+jsonPathName("B") {
		t.Fatal("flat and nested names collide")
	}
}

func TestDTOValidationPathNamingIsStable(t *testing.T) {
	source := `package sample
 type Left_Node struct { Value string ` + "`json:\"left\"`" + ` }
 type Right struct { Value string ` + "`json:\"right\"`" + ` }
 //foundry:dto
 type Payload struct { Left_Node; Right; Next *Payload; Counts map[string]int; Values []string }
 func usage(){ f:=PayloadValidationFields(); _=f.Field_Left__Node_Value; _=f.Field_Right_Value }
 `
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("path naming changed")
	}
	write(t, dir, "models.go", strings.Replace(source, "Next *Payload", "Extra bool; Next *Payload", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestDTOJSONLocalsDoNotShadowPayloadTypes(t *testing.T) {
	dir := fixture(t, `package sample
 type key int
 type p struct{ Entries map[key]string }
 type f struct{ Value p }
 type field struct{ Next f }
 type path struct{ Root field }
 //foundry:dto
 type Payload struct{ Settings f; Data path }
 `)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
}

func TestDTOJSONRejectsMalformedTagName(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:dto\ntype Payload struct { Name string `json:\"odd'path\"` }\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
		t.Fatal("malformed name accepted")
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid JSON schema published output")
	}
}
