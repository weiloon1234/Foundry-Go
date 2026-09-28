package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestFreshFormGeneration(t *testing.T) {
	source := strings.ReplaceAll(strings.ReplaceAll(querySource, "//foundry:query", "//foundry:form"), "query:\"", "form:\"")
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["search_input_foundry.gen.go"]
	for _, want := range []string{"Query[SearchInput]", "SearchInputValidationFields()", "Field[SearchInput, MemberID]", "OptionalQueryParam[SearchInput, string]", "RepeatedQueryParam[SearchInput, State, States]"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q: %s", want, output)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("form output is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidFormDeclarations(t *testing.T) {
	for name, source := range map[string]string{
		"pointer":     "//foundry:form\ntype Input struct{Name *string}",
		"nullable":    "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:form\ntype Input struct{Name value.Nullable[string]}",
		"duplicate":   "//foundry:form\ntype Input struct{A string `form:\"same\"`; B string `form:\"same\"`}",
		"collision":   "type InputValidationFieldSet struct{}\n//foundry:form\ntype Input struct{Name string}",
		"json-option": "//foundry:form\ntype Input struct{Name string `form:\",json\"`}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid form accepted")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid form published output")
			}
		})
	}
}
