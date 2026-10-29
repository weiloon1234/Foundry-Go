package validation_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestFieldLabelsKeepWirePathsAndIndependentDeclarations(t *testing.T) {
	t.Parallel()
	field := validation.DefineField("display~/name", func(p Patch) Contact { return p.Name })
	labeled := validation.WithLabel(field, "Display name")
	rule := validation.Rules(labeled, validation.WithMessage(validation.NonBlank[Contact](), "Enter a name."))
	issue := rejection(t, validation.Check(rule, t.Context(), Patch{}, validation.DefaultLimits())).Issues()[0]
	if issue.Path != "/display~0~1name" || issue.Label != "Display name" || issue.Message != "Enter a name." {
		t.Fatalf("label changed public identity or message: %+v", issue)
	}
	plain := rejection(t, validation.Check(validation.Rules(field, validation.NonBlank[Contact]()), t.Context(), Patch{}, validation.DefaultLimits())).Issues()[0]
	if plain.Label != "" {
		t.Fatal("label changed the original field")
	}
	metadata, err := validation.DescribeRule(rule)
	if err != nil || metadata.Field != "display~/name" || metadata.Label != "Display name" {
		t.Fatalf("metadata disagrees with diagnostics: %+v, %v", metadata, err)
	}
	metadata.Label = "changed"
	again, _ := validation.DescribeRule(rule)
	if again.Label != "Display name" {
		t.Fatal("description mutation changed the rule")
	}
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]string
	if err := json.Unmarshal(encoded, &wire); err != nil || wire["label"] != "Display name" || wire["path"] != issue.Path {
		t.Fatalf("wire label: %s, %v", encoded, err)
	}
	encoded, err = json.Marshal(contract.Issue{Path: "/name", Code: contract.TypeIssue})
	if err != nil || strings.Contains(string(encoded), "label") {
		t.Fatalf("shape errors acquired a label: %s, %v", encoded, err)
	}
}

func TestLabelsAreScopedToFieldsAndScalarCollectionItems(t *testing.T) {
	t.Parallel()
	type child struct{ Named, Plain string }
	type input struct {
		Tags  []string
		Child child
		Tail  string
	}
	tags := validation.WithLabel(validation.DefineField("tags", func(v input) []string { return v.Tags }), "Tags")
	nested := validation.WithLabel(validation.DefineField("child", func(v input) child { return v.Child }), "Child object")
	named := validation.WithLabel(validation.DefineField("named", func(v child) string { return v.Named }), "Child name")
	plain := validation.DefineField("plain", func(v child) string { return v.Plain })
	tail := validation.DefineField("tail", func(v input) string { return v.Tail })
	rule := validation.All(
		validation.Rules(tags, validation.Each[[]string](validation.NonBlank[string]())),
		validation.Rules(nested, validation.Rules(named, validation.NonBlank[string]()), validation.Rules(plain, validation.NonBlank[string]())),
		validation.Rules(tail, validation.NonBlank[string]()),
	)
	issues := rejection(t, validation.Check(rule, t.Context(), input{Tags: []string{""}}, validation.DefaultLimits())).Issues()
	want := []struct{ path, label string }{{"/tags/0", "Tags"}, {"/child/named", "Child name"}, {"/child/plain", ""}, {"/tail", ""}}
	if len(issues) != len(want) {
		t.Fatalf("issues: %+v", issues)
	}
	for i, expected := range want {
		if issues[i].Path != expected.path || issues[i].Label != expected.label {
			t.Fatalf("label scope at %d: %+v", i, issues[i])
		}
	}
}

func TestComparisonAndConditionalLabelsRetainTheirFieldOwners(t *testing.T) {
	t.Parallel()
	type input struct{ Password, Confirmation string }
	password := validation.WithLabel(validation.DefineField("password", func(v input) string { return v.Password }), "Password")
	confirmation := validation.WithLabel(validation.DefineField("confirmation", func(v input) string { return v.Confirmation }), "Confirm password")
	comparison := validation.Compare(confirmation, password, validation.Same[string]())
	rule := validation.When(validation.Rules(password, validation.NonBlank[string]()), comparison)
	issues := rejection(t, validation.Check(rule, t.Context(), input{Password: "private-password", Confirmation: "different"}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/confirmation" || issues[0].Label != "Confirm password" {
		t.Fatalf("comparison used condition/other label: %+v", issues)
	}
	metadata, err := validation.DescribeRule(comparison)
	if err != nil || metadata.Label != "Confirm password" || metadata.OtherLabel != "Password" {
		t.Fatalf("comparison labels missing: %+v, %v", metadata, err)
	}
	encoded, err := json.Marshal(issues)
	if err != nil || strings.Contains(string(encoded), "private-password") {
		t.Fatalf("input leaked into public label: %s, %v", encoded, err)
	}
}

func TestInvalidAndOversizedLabelsFailBeforeExecution(t *testing.T) {
	t.Parallel()
	field := validation.DefineField("name", func(p Patch) Contact { return p.Name })
	for _, label := range []string{"", " \t\n", "bad\x00name", string([]byte{255}), strings.Repeat("x", 16385)} {
		if validation.ValidateRule(validation.Rules(validation.WithLabel(field, label), validation.NonBlank[Contact]())) == nil {
			t.Fatal("invalid label accepted")
		}
	}
	if validation.ValidateField(validation.WithLabel((validation.Field[Patch, Contact]{}), "Name")) == nil {
		t.Fatal("label made an undefined field valid")
	}
	// Reusing large labels remains subject to the existing aggregate metadata
	// limit; accepted individual labels must not bypass the rule tree bound.
	rule := validation.Rules(validation.WithLabel(field, strings.Repeat("x", 16384)), validation.NonBlank[Contact]())
	children := make([]validation.Rule[Patch], 65)
	for i := range children {
		children[i] = rule
	}
	if validation.ValidateRule(validation.All(children...)) == nil {
		t.Fatal("labels escaped the aggregate metadata bound")
	}
}
