package localization

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/validation"
	"testing"
)

func TestGeneratedValidationMessageOverrideAndNonHTTPPresentation(t *testing.T) {
	catalog, err := ValidationCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rule := NameRule()
	description, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.ValidateMessages(description, catalog); err != nil {
		t.Fatal(err)
	}
	var rejected *validation.Errors
	if err := rule.Check(t.Context(), WelcomeArgs{}, validation.DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	result, err := rejected.Localize(t.Context(), catalog, "ms")
	if err != nil {
		t.Fatal(err)
	}
	if result.Issues()[0].Message != "Nama diperlukan oleh Foundry team." || result.Issues()[0].Path != "/name" {
		t.Fatal(result.Issues())
	}
	fallback, err := rejected.Localize(t.Context(), catalog, "en")
	if err != nil || fallback.Issues()[0].Message != "Enter your name." {
		t.Fatal(fallback, err)
	}
	override := validation.WithTranslation(validation.NonBlank[string](), RuleArgsMessage(), RuleArgs{Team: "team"}).WithMessage("literal {{team}}")
	if err := override.Check(t.Context(), "", validation.DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	result, err = rejected.Localize(t.Context(), catalog, "ms")
	if err != nil || result.Issues()[0].Message != "literal {{team}}" {
		t.Fatal(result, err)
	}
}

func TestPreparedTypedMessageOwnsItsFallback(t *testing.T) {
	prepared, err := PreparedWelcome(t.Context(), "Team")
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Format(t.Context(), nil, "")
	if err != nil || result.Text != "Welcome Team." {
		t.Fatal(result, err)
	}
}

func TestUnboundTypedMessageKeepsExplicitAttribute(t *testing.T) {
	catalog, err := ValidationCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rule := validation.WithTranslation(validation.Custom(validation.Spec{ID: "profile.name", Message: "Enter a name."}, func(context.Context, string) (bool, error) { return false, nil }), RuleArgsMessage(), RuleArgs{Attribute: "Alias", Team: "Team"})
	var rejected *validation.Errors
	if err := rule.Check(t.Context(), "private rejected input", validation.DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	result, err := rejected.Localize(t.Context(), catalog, "ms")
	if err != nil || result.Issues()[0].Message != "Alias diperlukan oleh Team." {
		t.Fatal(result, err)
	}
}
