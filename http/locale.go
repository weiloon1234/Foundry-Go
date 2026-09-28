package http

import (
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// Locale resolves the request's explicit context preference or Accept-Language
// against one immutable catalog and localizes shared error responses. Those
// responses set Content-Language and Vary; successful responses retain their
// own representation policy. No catalog or services are stored in context.
func Locale(catalog *i18n.Catalog) Middleware {
	return defineReplayMiddleware("foundry.locale", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := catalog.Validate(); err != nil {
			return nil, err
		}
		if described, ok := next.(interface{ Endpoints() []EndpointInfo }); ok {
			for _, endpoint := range described.Endpoints() {
				if endpoint.Validation != nil {
					if err := validation.ValidateMessages(*endpoint.Validation, catalog); err != nil {
						return nil, err
					}
				}
			}
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			preferred, _ := i18n.RequestLocale(r.Context())
			locale, err := catalog.Resolve(string(preferred), r.Header.Get("Accept-Language"))
			if err != nil {
				stdhttp.Error(w, "locale resolution failed", stdhttp.StatusInternalServerError)
				return
			}
			ctx, err := i18n.WithLocale(r.Context(), catalog, locale)
			if err != nil {
				stdhttp.Error(w, "locale resolution failed", stdhttp.StatusInternalServerError)
				return
			}
			writer := &localizedResponseWriter{ResponseWriter: w, presenter: errorPresenter{catalog: catalog, locale: locale}}
			next.ServeHTTP(responseCapabilities(writer), r.WithContext(ctx))
		}), nil
	})
}
