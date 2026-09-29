package http

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// LocaleMiddlewareID identifies built-in request locale selection.
const LocaleMiddlewareID MiddlewareID = "foundry.locale"

// maxLocaleSelectorBytes bounds an explicit query or cookie locale value.
const maxLocaleSelectorBytes = 64

// LocaleNegotiation declares explicit request locale sources. In order, a query
// parameter, a cookie and an application preference (for example an
// authenticated user's stored locale) are consulted before a locale inherited on
// the request context, then Accept-Language, then the catalog default. Every
// value is matched against the catalog: malformed or unsupported values defer to
// the next source (a supported parent tag may match). The zero value consults
// only the context locale and Accept-Language.
//
// Preferred is application code: it must honor the request context and be safe
// for concurrent use; its panics are contained and its errors are written as the
// shared error response. An authenticated preference needs the actor, so install
// LocaleWith on the authenticated route or scope, inside its authentication
// middleware, where it overrides a global Locale's selection.
type LocaleNegotiation struct {
	QueryParameter string
	Cookie         CookieName
	Preferred      func(*stdhttp.Request) (i18n.LocaleID, bool, error)
}

func (n LocaleNegotiation) Validate() error {
	if n.QueryParameter != "" && !httpquery.ValidName(n.QueryParameter) {
		return fault.New(fault.Invalid, "locale query parameter name is invalid")
	}
	if n.Cookie != "" {
		if err := n.Cookie.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Locale resolves the request's explicit context preference or Accept-Language
// against one immutable catalog and localizes shared error responses. Those
// responses set Content-Language and Vary; successful responses retain their
// own representation policy. No catalog or services are stored in context.
func Locale(catalog *i18n.Catalog) Middleware {
	return LocaleWith(catalog, LocaleNegotiation{})
}

// LocaleWith is Locale with explicit query, cookie and application sources.
// Responses selected by a query, cookie or user preference also depend on those
// inputs; keep such responses private or declare their cache variation.
func LocaleWith(catalog *i18n.Catalog, negotiation LocaleNegotiation) Middleware {
	return defineReplayMiddleware(LocaleMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := catalog.Validate(); err != nil {
			return nil, err
		}
		if err := negotiation.Validate(); err != nil {
			return nil, err
		}
		resolver, err := i18n.NewLocaleResolver(catalog, localeSteps(negotiation)...)
		if err != nil {
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
			ctx, resolution, err := resolver.WithResolvedLocale(r.Context(), r)
			if err != nil {
				if negotiation.Preferred != nil {
					writeRoutingError(w, r, err)
					return
				}
				stdhttp.Error(w, "locale resolution failed", stdhttp.StatusInternalServerError)
				return
			}
			if negotiation.QueryParameter != "" {
				// The selector is request metadata: typed query decoding and
				// signed link verification ignore it.
				ctx = withConsumedQueryParameter(ctx, negotiation.QueryParameter)
			}
			writer := &localizedResponseWriter{ResponseWriter: w, presenter: errorPresenter{catalog: catalog, locale: resolution.Locale}}
			next.ServeHTTP(responseCapabilities(writer), r.WithContext(ctx))
		}), nil
	})
}

func localeSteps(negotiation LocaleNegotiation) []i18n.Preference[*stdhttp.Request] {
	var steps []i18n.Preference[*stdhttp.Request]
	if name := negotiation.QueryParameter; name != "" {
		steps = append(steps, i18n.Preferred("query", func(_ context.Context, r *stdhttp.Request) (i18n.LocaleID, bool, error) {
			if r.URL == nil {
				return "", false, nil
			}
			return explicitLocale(singleQueryValue(r.URL.RawQuery, name))
		}))
	}
	if name := negotiation.Cookie; name != "" {
		steps = append(steps, i18n.Preferred("cookie", func(_ context.Context, r *stdhttp.Request) (i18n.LocaleID, bool, error) {
			text, present, err := findRequestCookie(r.Header.Values("Cookie"), name)
			if err != nil || !present {
				return "", false, nil
			}
			return explicitLocale(text, true)
		}))
	}
	if preferred := negotiation.Preferred; preferred != nil {
		steps = append(steps, i18n.Preferred("preference", func(ctx context.Context, r *stdhttp.Request) (i18n.LocaleID, bool, error) {
			return preferred(r.WithContext(ctx))
		}))
	}
	return append(steps,
		i18n.ContextLocale[*stdhttp.Request](),
		i18n.AcceptLanguage(func(r *stdhttp.Request) string { return r.Header.Get("Accept-Language") }),
	)
}

// explicitLocale canonicalizes client-supplied locale text. Malformed text
// defers to the next source instead of failing the request.
func explicitLocale(text string, present bool) (i18n.LocaleID, bool, error) {
	if !present || text == "" || len(text) > maxLocaleSelectorBytes {
		return "", false, nil
	}
	locale, err := i18n.ParseLocale(text)
	if err != nil {
		return "", false, nil
	}
	return locale, true, nil
}

// singleQueryValue scans raw query pairs without parsing the whole query. A
// missing, repeated or undecodable selector reports absence.
func singleQueryValue(raw, name string) (string, bool) {
	var found string
	present := false
	for pair := range strings.SplitSeq(raw, "&") {
		key, text, _ := strings.Cut(pair, "=")
		if decoded, err := url.QueryUnescape(key); err != nil || decoded != name {
			continue
		}
		if present {
			return "", false
		}
		value, err := url.QueryUnescape(text)
		if err != nil {
			return "", false
		}
		found, present = value, true
	}
	return found, present
}
