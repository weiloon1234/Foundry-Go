package generate

import (
	"go/ast"
	"strings"
	"testing"
)

func TestDirectiveSpellingDiagnostic(t *testing.T) {
	const correction = "invalid Foundry directive spelling; use //foundry:model"
	for _, tc := range []struct{ comment, want string }{
		{"//foudry:model table=x", correction},
		{"//foundry-model table=x", correction},
		{"//foundry :model table=x", correction},
		{"//foudry:model users", correction},
		{"//foundry-model users", correction},
		{"//foundry :model users", correction},
		{"// foudry:model", correction},
		{"// foudry:model table=x", correction},
		{"// foundry-model table=x", correction},
		{"// foundry :model table=x", correction},
		{"// foundry:model table=x", correction},
		{"//\tfoudry:model table=x", correction},
		{"// foudry:model users", ""},
		{"// foudry:model table users", ""},
		{"// foundry-model users", ""},
		{"// foundry :model of an account", ""},
		{"// found: model of an account", ""},
		{"// boundary: model", ""},
		{"//foundry:unknown", "unsupported Foundry declaration directive"},
		{"//foundry:model users", "directive options require key=value"},
	} {
		t.Run(tc.comment, func(t *testing.T) {
			group := &ast.CommentGroup{List: []*ast.Comment{{Text: tc.comment}}}
			kind, _, err := directive(group)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("diagnostic = %q, want %q", got, tc.want)
			}
			if tc.want == "" && kind != "" {
				t.Fatalf("prose became a %s declaration", kind)
			}
			if got := mayDeclare([]byte(tc.comment)); got != (tc.want != "") {
				t.Fatalf("prefilter = %t, want %t", got, tc.want != "")
			}
		})
	}
}

func TestDirectivePrefilterAcceptsEveryParsedCommentForm(t *testing.T) {
	for text, want := range map[string]bool{
		"//foundry:dto":                true,
		"// foundry:dto":               true,
		"//\t foundry:enum":            true,
		"// foundry:dto":               true,
		"code // note\n// foundry:x":   false,
		"x := \"foundry:dto\"":         false,
		"//\nfoundry:dto":              false,
		"/* foundry:dto */":            false,
		"package sample":               false,
		"// Foundry-Go owns this type": false,
	} {
		if got := mayDeclare([]byte(text)); got != want {
			t.Errorf("mayDeclare(%q) = %t, want %t", text, got, want)
		}
	}
}

// Near misses must reach discovery so malformed declarations cannot disappear.
func TestSpacedDirectivesAreDiscovered(t *testing.T) {
	dir := fixture(t, "package sample\n\n// foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n\n// foundry:dto\ntype Payload struct{ Mode Mode `json:\"mode\"` }\n")
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "use //foundry:enum") {
		t.Fatalf("spaced directive was not rejected: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid directive published output")
	}
	constrained := fixture(t, "package sample\n")
	write(t, constrained, "mode_platform.go", "//go:build foundry_never_selected\n\npackage sample\n\n// foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n")
	if _, err := generateFixture(t.Context(), Options{Dir: constrained}); err == nil || !strings.Contains(err.Error(), "build constraints") {
		t.Fatalf("constrained spaced directive was not reported: %v", err)
	}
}

func TestDirectiveProseIsNotDeclaration(t *testing.T) {
	for _, comment := range []string{"// found: model of an account", "// foundry-model: comment", "// foundry: model of an account", "// foudry: model of an account", "// boundary: model", "// foundry_ui: model"} {
		if _, ok := directiveComment(comment); ok {
			t.Errorf("prose became a directive: %s", comment)
		}
	}
}
