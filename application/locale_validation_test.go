package application_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

func TestLocaleDefaultMustBeConfigured(t *testing.T) {
	for _, test := range []struct {
		name   string
		locale i18n.LocaleID
		want   error
	}{
		{name: "default", locale: "en"},
		{name: "different configured locale", locale: "ms"},
		{name: "unconfigured locale", locale: "fr", want: fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := settings()
			s.HTTP.Enabled = false
			s.Features.Locales = application.LocaleSettings{
				Enabled: true, Default: test.locale, Locales: []i18n.LocaleID{"en", "ms"},
			}
			app, err := application.New(s, quiet()).Build(t.Context())
			if err == nil {
				t.Cleanup(func() { stop(t, app) })
			}
			if !errors.Is(err, test.want) {
				t.Fatal("unexpected locale configuration result", err)
			}
			if test.want != nil {
				var safe *fault.Error
				if !errors.As(err, &safe) {
					t.Fatal("configuration failure is not a safe fault", err)
				}
			}
		})
	}
}
