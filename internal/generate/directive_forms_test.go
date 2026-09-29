package generate

import (
	"strings"
	"testing"
)

func TestDirectivePrefilterAcceptsEveryParsedCommentForm(t *testing.T) {
	for text, want := range map[string]bool{
		"//foundry:model":            true,
		"// foundry:model":           true,
		"//\t foundry:enum":          true,
		"// foundry:dto":             true,
		"code // note\n// foundry:x": true,
		"x := \"foundry:model\"":     false,
		"//\nfoundry:model":          false,
		"/* foundry:model */":        false,
		"package sample":             false,
	} {
		if got := mayDeclare([]byte(text)); got != want {
			t.Errorf("mayDeclare(%q) = %t, want %t", text, got, want)
		}
	}
}

// The spaced form is accepted by directive parsing, so the fast path must not
// skip declaration checking for a package that uses only that form.
func TestSpacedDirectivesAreDiscovered(t *testing.T) {
	dir := fixture(t, "package sample\n\n// foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n\n// foundry:dto\ntype Payload struct{ Mode Mode `json:\"mode\"` }\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	outputs := generatedSnapshot(t, dir)
	if !strings.Contains(outputs["mode_foundry.gen.go"], "EnumDescriptor") || !strings.Contains(outputs["payload_foundry.gen.go"], "func PayloadJSON()") {
		t.Fatalf("spaced directives were not generated: %v", sortedNames(outputs))
	}
	constrained := fixture(t, "package sample\n")
	write(t, constrained, "mode_platform.go", "//go:build foundry_never_selected\n\npackage sample\n\n// foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n")
	if _, err := Generate(t.Context(), Options{Dir: constrained}); err == nil || !strings.Contains(err.Error(), "build constraints") {
		t.Fatalf("constrained spaced directive was not reported: %v", err)
	}
}
