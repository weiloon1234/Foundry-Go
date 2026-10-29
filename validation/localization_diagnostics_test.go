package validation_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestLabelLocalizationFailureDiagnosticsSurviveContainment(t *testing.T) {
	set, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := i18n.NewCatalog(t.Context(), set, i18n.CatalogOptions{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rejected := rejection(t, validation.Check(validation.NonBlank[string](), t.Context(), "", validation.DefaultLimits()))
	for _, test := range []struct {
		name     string
		receiver *validation.Errors
		ctx      context.Context
		locale   i18n.LocaleID
		message  string
	}{
		{"nil receiver", nil, t.Context(), "en", "labels: invalid: invalid validation label localization"},
		{"nil context", rejected, nil, "en", "labels: invalid: invalid validation label localization"},
		{"unsupported locale", rejected, t.Context(), "ms", "labels: invalid: unsupported validation locale"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var original error
			result := callback.Isolated("labels", func() error {
				value, err := test.receiver.LocalizeLabels(test.ctx, catalog, test.locale)
				if value != nil {
					t.Error("failed localization exposed partial diagnostics")
				}
				original = err
				return err
			})
			var source *fault.Error
			if original == nil || !errors.Is(result, original) || !errors.Is(result, fault.Invalid) || !errors.As(result, &source) || !errors.Is(result, source) {
				t.Fatal("label localization lost failure classification or identity")
			}
			*source = *fault.New(fault.Invalid, "private replacement")
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != test.message {
					t.Fatalf("localization diagnostic = %q, want %q", got, test.message)
				}
			}
		})
	}
}
