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
	labeled := field.WithLabel("Display name")
	rule := labeled.Rules(validation.NonBlank[Contact]().WithMessage("Enter a name."))
	issue := rejection(t, rule.Check(t.Context(), Patch{}, validation.DefaultLimits())).Issues()[0]
	if issue.Path != "/display~0~1name" || issue.Label != "Display name" || issue.Message != "Enter a name." {
		t.Fatalf("label changed public identity or message: %+v", issue)
	}
	plain := rejection(t, field.Rules(validation.NonBlank[Contact]()).Check(t.Context(), Patch{}, validation.DefaultLimits())).Issues()[0]
	if plain.Label != "" {
		t.Fatal("label changed the original field")
	}
	metadata, err := rule.Description()
	if err != nil || metadata.Field != "display~/name" || metadata.Label != "Display name" {
		t.Fatalf("metadata disagrees with diagnostics: %+v, %v", metadata, err)
	}
	metadata.Label = "changed"
	again, _ := rule.Description()
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
	tags := validation.DefineField("tags", func(v input) []string { return v.Tags }).WithLabel("Tags")
	nested := validation.DefineField("child", func(v input) child { return v.Child }).WithLabel("Child object")
	named := validation.DefineField("named", func(v child) string { return v.Named }).WithLabel("Child name")
	plain := validation.DefineField("plain", func(v child) string { return v.Plain })
	tail := validation.DefineField("tail", func(v input) string { return v.Tail })
	rule := validation.All(
		tags.Rules(validation.Each[[]string](validation.NonBlank[string]())),
		nested.Rules(named.Rules(validation.NonBlank[string]()), plain.Rules(validation.NonBlank[string]())),
		tail.Rules(validation.NonBlank[string]()),
	)
	issues := rejection(t, rule.Check(t.Context(), input{Tags: []string{""}}, validation.DefaultLimits())).Issues()
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
	password := validation.DefineField("password", func(v input) string { return v.Password }).WithLabel("Password")
	confirmation := validation.DefineField("confirmation", func(v input) string { return v.Confirmation }).WithLabel("Confirm password")
	comparison := validation.Compare(confirmation, password, validation.Same[string]())
	rule := validation.When(password.Rules(validation.NonBlank[string]()), comparison)
	issues := rejection(t, rule.Check(t.Context(), input{Password: "private-password", Confirmation: "different"}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/confirmation" || issues[0].Label != "Confirm password" {
		t.Fatalf("comparison used condition/other label: %+v", issues)
	}
	metadata, err := comparison.Description()
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
		if field.WithLabel(label).Rules(validation.NonBlank[Contact]()).Validate() == nil {
			t.Fatal("invalid label accepted")
		}
	}
	if (validation.Field[Patch, Contact]{}).WithLabel("Name").Validate() == nil {
		t.Fatal("label made an undefined field valid")
	}
	// Reusing large labels remains subject to the existing aggregate metadata
	// limit; accepted individual labels must not bypass the rule tree bound.
	rule := field.WithLabel(strings.Repeat("x", 16384)).Rules(validation.NonBlank[Contact]())
	children := make([]validation.Rule[Patch], 65)
	for i := range children {
		children[i] = rule
	}
	if validation.All(children...).Validate() == nil {
		t.Fatal("labels escaped the aggregate metadata bound")
	}
}
