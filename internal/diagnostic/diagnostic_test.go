package diagnostic

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var rule = Rule{"sample-rule", "sample-guide", "sample-anchor", ""}

func TestFindingShape(t *testing.T) {
	for _, test := range []struct {
		name string
		d    Diagnostic
		want string
	}{
		{"full", rule.At("a/b.go", 4, 7).Say("http.Scope", "stage nested").To("assign a local"),
			"a/b.go:4:7: [sample-rule] http.Scope: stage nested; assign a local. Foundry guide: sample-guide#sample-anchor"},
		{"no column", rule.At("a.md", 9, 0).Say("link", "broken").To("fix it"),
			"a.md:9: [sample-rule] link: broken; fix it. Foundry guide: sample-guide#sample-anchor"},
		{"no line or column", rule.At("a.md", 0, 5).Say("file", "unreadable").To("restore it"),
			"a.md: [sample-rule] file: unreadable; restore it. Foundry guide: sample-guide#sample-anchor"},
		{"no subject", rule.At("a.md", 2, 0).Text("", "orm.X is retired; use orm.Y."),
			"a.md:2: [sample-rule] orm.X is retired; use orm.Y. Foundry guide: sample-guide#sample-anchor"},
		{"no fix", rule.At("a.md", 2, 0).Say("x", "bad."),
			"a.md:2: [sample-rule] x: bad. Foundry guide: sample-guide#sample-anchor"},
		{"skills", rule.At("a.md", 2, 0).Say("x", "bad").To("fix").WithSkills(Skills{"sample-guide#sample-anchor": {"a", "b"}}),
			"a.md:2: [sample-rule] x: bad; fix. Foundry guide: sample-guide#sample-anchor (skill: a, b)"},
		{"unnamed skills", rule.At("a.md", 2, 0).Say("x", "bad").To("fix").WithSkills(Skills{"other#anchor": {"a"}}),
			"a.md:2: [sample-rule] x: bad; fix. Foundry guide: sample-guide#sample-anchor"},
	} {
		if got := test.d.String(); got != test.want || test.d.Error() != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
		if strings.Contains(test.d.String(), ":0") {
			t.Errorf("%s prints a zero location: %q", test.name, test.d)
		}
	}
}

func TestApplicationFindingsTeachTheApplicationReference(t *testing.T) {
	both := Rule{"both-rule", "sample-guide", "sample-anchor", "app-anchor"}
	skills := Skills{"sample-guide#app-anchor": {"web"}, "sample-guide#sample-anchor": {"author"}}
	for _, test := range []struct {
		name string
		d    Diagnostic
		want string
	}{
		{"framework", both.At("a.md", 1, 0).Say("x", "bad").To("fix"), "a.md:1: [both-rule] x: bad; fix. Foundry guide: sample-guide#sample-anchor"},
		{"application", both.At("a.md", 1, 0).Say("x", "bad").To("fix").InApplication(), "a.md:1: [both-rule] x: bad; fix. Foundry guide: sample-guide#app-anchor"},
		{"application skills", both.At("a.md", 1, 0).Say("x", "bad").To("fix").InApplication().WithSkills(skills), "a.md:1: [both-rule] x: bad; fix. Foundry guide: sample-guide#app-anchor (skill: web)"},
		{"framework skills", both.At("a.md", 1, 0).Say("x", "bad").To("fix").WithSkills(skills), "a.md:1: [both-rule] x: bad; fix. Foundry guide: sample-guide#sample-anchor (skill: author)"},
		{"no application anchor", rule.At("a.md", 1, 0).Say("x", "bad").To("fix").InApplication(), "a.md:1: [sample-rule] x: bad; fix. Foundry guide: sample-guide#sample-anchor"},
	} {
		if got := test.d.String(); got != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
	// The mark survives the error round trip that commands print from.
	found, _ := Findings(both.At("a.md", 1, 0).Say("x", "bad").InApplication().Err())
	if len(found) != 1 || found[0].Reference() != "sample-guide#app-anchor" {
		t.Fatalf("lost the application mark: %+v", found)
	}
}

func TestSkillsAreSortedAndUnique(t *testing.T) {
	skills := Skills{}
	for _, name := range []string{"b", "a", "b", "c"} {
		skills.Add("g#a", name)
	}
	if got := strings.Join(skills["g#a"], ","); got != "a,b,c" {
		t.Fatalf("skills = %s", got)
	}
}

func TestFindingsSurviveJoinsAndCauses(t *testing.T) {
	cause := errors.New("disk")
	first := rule.At("a.md", 1, 0).Say("x", "bad").To("fix").Err()
	second := rule.At("b.md", 2, 0).Say("y", "worse").To("fix").Caused(cause)
	joined := safeerror.Join("failed", first, nil, safeerror.Join("failed", second, safeerror.New("plain owned text")))
	found, other := Findings(joined)
	if len(found) != 2 || found[0].Path != "a.md" || found[1].Path != "b.md" || len(other) != 1 || other[0] != "plain owned text" {
		t.Fatalf("findings = %+v other = %v", found, other)
	}
	if !errors.Is(second, cause) {
		t.Fatal("cause lost")
	}
	if found, other := Findings(errors.New("foreign")); len(found) != 0 || len(other) != 1 || strings.Contains(other[0], "foreign") {
		t.Fatalf("foreign text leaked: %v %v", found, other)
	}
}

func TestAnnotateNamesSkillsOnlyForFindings(t *testing.T) {
	err := safeerror.Join("failed", rule.At("a.md", 1, 0).Say("x", "bad").To("fix").Err(), rule.At("b.md", 2, 0).Say("y", "worse").To("fix").Err())
	called := 0
	skills := func() Skills { called++; return Skills{"sample-guide#sample-anchor": {"workflow"}} }
	got := Annotate(err, skills)
	lines := strings.Split(got.Error(), "\n")
	if called != 1 || len(lines) != 2 || !strings.HasSuffix(lines[0], "(skill: workflow)") || !strings.HasSuffix(lines[1], "(skill: workflow)") || !errors.Is(got, errors.Unwrap(got)) {
		t.Fatalf("annotated = %q (skills read %d times)", got, called)
	}
	plain := errors.New("no findings")
	if Annotate(plain, func() Skills { t.Fatal("skills read without findings"); return nil }) != plain || Annotate(nil, nil) != nil {
		t.Fatal("an error without findings changed")
	}
}

// Every rule is listed once, with valid identifiers, and every Rule variable of
// rule.go is in the list.
func TestRuleCatalogueIsComplete(t *testing.T) {
	name := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	seen := map[string]bool{}
	for _, r := range Rules() {
		if !name.MatchString(r.ID) || !name.MatchString(r.Guide) || !name.MatchString(r.Anchor) || r.Application != "" && (!name.MatchString(r.Application) || r.Application == r.Anchor) || seen[r.ID] {
			t.Errorf("invalid or repeated rule %+v", r)
		}
		seen[r.ID] = true
	}
	file, err := parser.ParseFile(token.NewFileSet(), "rule.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := 0
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.ValueSpec); ok {
			for _, value := range spec.Values {
				if literal, ok := value.(*ast.CompositeLit); ok {
					if id, ok := literal.Type.(*ast.Ident); ok && id.Name == "Rule" {
						declared++
					}
				}
			}
		}
		return true
	})
	if declared != len(Rules()) {
		t.Fatalf("rule.go declares %d rules, Rules() lists %d", declared, len(Rules()))
	}
}
