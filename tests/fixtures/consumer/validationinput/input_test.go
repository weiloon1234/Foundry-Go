package validationinput_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/validationinput"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestTypedConsumerValidation(t *testing.T) {
	if err := validationinput.Rules.Check(t.Context(), validationinput.Request{Name: "Member", Age: 35}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	err := validationinput.Rules.Check(t.Context(), validationinput.Request{Name: " ", Age: 12}, validation.DefaultLimits())
	var rejected *validation.Errors
	if !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	issues := rejected.Issues()
	if len(issues) != 2 || issues[0].Path != "/displayName" || issues[1].Path != "/age" {
		t.Fatalf("consumer diagnostics: %+v", issues)
	}
	description, err := validationinput.Rules.Description()
	if err != nil {
		t.Fatal(err)
	}
	if description.Kind != validation.AllKind || len(description.Children) != 2 || description.Children[0].Field != "displayName" {
		t.Fatal("consumer runtime and metadata disagree")
	}
}
