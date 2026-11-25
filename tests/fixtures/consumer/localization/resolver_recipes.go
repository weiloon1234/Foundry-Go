package localization

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/i18n"
)

// doc-example:begin locale-recipient
// Recipient supplies a saved preference and the request language header.
type Recipient struct {
	Locale         i18n.LocaleID
	AcceptLanguage string
}

// doc-example:end locale-recipient

// doc-example:begin resolve-recipient-locale
func resolveRecipientLocale(ctx context.Context, catalog *i18n.Catalog, recipient *Recipient) (context.Context, i18n.Resolution, error) {
	resolver, err := i18n.NewLocaleResolver(catalog,
		i18n.ContextLocale[*Recipient](),
		i18n.Preferred("user", func(ctx context.Context, r *Recipient) (i18n.LocaleID, bool, error) {
			return r.Locale, r.Locale != "", nil
		}),
		i18n.AcceptLanguage(func(r *Recipient) string { return r.AcceptLanguage }),
	)
	if err != nil {
		return ctx, i18n.Resolution{}, err
	}
	resolution, err := resolver.Resolve(ctx, recipient)
	if err != nil {
		return ctx, resolution, err
	}
	// WithResolvedLocale also attaches the supported locale to the context.
	return resolver.WithResolvedLocale(ctx, recipient)
}

// doc-example:end resolve-recipient-locale
