package application_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

func TestLocaleFallbackMustMatchDefault(t *testing.T) {
	for _, test := range []struct {
		name     string
		fallback i18n.LocaleID
		want     error
	}{
		{name: "empty"},
		{name: "default", fallback: "en"},
		{name: "different configured locale", fallback: "ms", want: fault.Invalid},
		{name: "unconfigured locale", fallback: "fr", want: fault.Missing},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := settings()
			s.HTTP.Enabled = false
			s.Features.Locales = application.LocaleSettings{
				Enabled: true, Default: "en", Locales: []i18n.LocaleID{"en", "ms"}, Fallback: test.fallback,
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
