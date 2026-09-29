package localization_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// Recipient is a consumer-owned notification target with a stored preference.
type Recipient struct {
	Locale         i18n.LocaleID
	AcceptLanguage string
}

func TestLocaleResolverServesStoredPreferencesAndHeaders(t *testing.T) {
	catalog, err := localization.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("profile store unavailable")
	resolver, err := i18n.NewLocaleResolver(catalog,
		i18n.ContextLocale[*Recipient](),
		i18n.Preferred("user", func(_ context.Context, r *Recipient) (i18n.LocaleID, bool, error) {
			if r == nil {
				return "", false, unavailable
			}
			return r.Locale, r.Locale != "", nil
		}),
		i18n.AcceptLanguage(func(r *Recipient) string { return r.AcceptLanguage }),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		recipient *Recipient
		want      i18n.Resolution
	}{
		{&Recipient{Locale: "ms-MY"}, i18n.Resolution{Locale: "ms", Source: "user"}},
		{&Recipient{Locale: "fr", AcceptLanguage: "ar-EG,en;q=0.5"}, i18n.Resolution{Locale: "ar", Source: i18n.AcceptLanguageSource}},
		{&Recipient{}, i18n.Resolution{Locale: "en", Source: i18n.DefaultSource}},
	} {
		ctx, resolution, err := resolver.WithResolvedLocale(t.Context(), test.recipient)
		if err != nil || resolution != test.want {
			t.Fatal(test.recipient, resolution, err)
		}
		locale, _ := i18n.RequestLocale(ctx)
		result, err := localization.Welcome(ctx, catalog, locale, "Ada")
		if err != nil || result.Missing {
			t.Fatal(result, err)
		}
	}
	if _, err := resolver.Resolve(t.Context(), nil); !errors.Is(err, unavailable) {
		t.Fatal("preference failure selected a default", err)
	}
}
