package application

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"log/slog"
	stdhttp "net/http"
	"slices"
)

func registerResources(builder *foundation.Builder, image ImageSettings, logger *slog.Logger, channels *logging.Channels, features FeatureSettings, source clock.Clock, recorder *observability.Recorder, dates temporal.Service, calendar schedule.Calendar) {
	requires := []foundation.ProviderID{infrastructure.Provider}
	if image.Enabled {
		builder.Register(imaging.Module(ImageProvider, ImageKey, image.Config))
		requires = append(requires, ImageProvider)
	}
	builder.Register(foundation.Module{Name: Provider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, servicesKey, func(r foundation.Resolver) (Services, error) {
			resources, err := foundation.Resolve(r, infrastructure.ServicesKey)
			if err != nil {
				return Services{}, err
			}
			result := Services{Services: resources, Logger: logger, Logs: channels, features: features, clock: source, recorder: recorder, dates: dates, calendar: calendar}
			if image.Enabled {
				result.image, err = foundation.Resolve(r, ImageKey)
			}
			return result, err
		})
	}})

}

// StickyReadsMiddlewareID identifies the database.StickyReadsHandler that the
// configured HTTP kernel installs outermost when a database connection with a
// read pool sets sticky_read_window.
const StickyReadsMiddlewareID http.MiddlewareID = "foundry.database.sticky_reads"

// stickyReadsConfigured reports whether any connection routes reads to a
// replica with a sticky window; without one the handler would have no effect.
func stickyReadsConfigured(s infrastructure.DatabaseSettings) bool {
	for _, connection := range s.Connections {
		if connection.ReadEnabled && connection.StickyReadWindow > 0 {
			return true
		}
	}
	return false
}

func registerHTTP(builder *foundation.Builder, settings HTTPSettings, localesEnabled, stickyReads bool, routes []Routes, spas []spaDeclaration, middleware []http.Middleware, observers []http.RequestObserver) {
	if settings.Enabled {
		builder.Register(foundation.Module{Name: RouterProvider, Requires: []foundation.ProviderID{Provider}, OnRegister: func(r *foundation.Registrar) error {
			for _, spa := range spas {
				if err := http.RegisterSPA(r, RouterKey, spa.id, spa.assets, spa.config); err != nil {
					return err
				}
			}
			return http.RegisterRouterWithRoutes(r, RouterKey, func(r foundation.Resolver) ([]http.RouteRegistration, error) {
				services, err := FromResolver(r)
				if err != nil {
					return nil, err
				}
				var registrations []http.RouteRegistration
				for _, construct := range routes {
					part, err := construct(services)
					if err != nil {
						return nil, err
					}
					registrations = append(registrations, part...)
				}
				return registrations, nil
			})
		}})
		global := slices.Clone(middleware)
		if settings.SecurityHeaders {
			global = append([]http.Middleware{http.SecurityHeaders(http.DefaultSecurityHeadersConfig())}, global...)
		}
		if stickyReads {
			// Outermost, so every route handler and middleware write shares
			// the request's read-your-writes scope.
			global = append([]http.Middleware{http.DefineMiddleware(StickyReadsMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
				return database.StickyReadsHandler(next), nil
			})}, global...)
		}
		serverOptions := make([]http.ServerOption, 0, len(observers)+1)
		for _, observer := range observers {
			serverOptions = append(serverOptions, http.WithRequestObserver(observer))
		}
		// Maintenance admission runs before the chain; reuse the global
		// TrustedProxy so allow networks match the real client address.
		for _, m := range global {
			if m.ID() == http.TrustedProxyMiddlewareID {
				serverOptions = append(serverOptions, http.WithAdmissionProxy(m))
			}
		}
		module := http.Module(HTTPProvider, HTTPKey, settings.Server, func(r foundation.Resolver) (stdhttp.Handler, error) {
			router, err := foundation.Resolve(r, RouterKey)
			if err != nil {
				return nil, err
			}
			if err := validateBrowserPolicies(router, global); err != nil {
				return nil, err
			}
			if localesEnabled {
				catalog, err := foundation.Resolve(r, LocaleKey)
				if err != nil {
					return nil, err
				}
				for _, endpoint := range router.Endpoints() {
					if endpoint.Validation != nil {
						if err := validation.ValidateMessages(*endpoint.Validation, catalog); err != nil {
							return nil, err
						}
					}
				}
				hasLocale := false
				for _, m := range global {
					hasLocale = hasLocale || m.ID() == "foundry.locale"
				}
				if !hasLocale {
					return http.ApplyMiddleware(router, append([]http.Middleware{http.Locale(catalog)}, global...)...)
				}
			}
			return http.ApplyMiddleware(router, global...)
		}, serverOptions...)
		module.Requires = []foundation.ProviderID{RouterProvider}
		if localesEnabled {
			module.Requires = append(module.Requires, LocaleProvider)
		}
		builder.Register(module)
	}

}

func validateBrowserPolicies(router *http.Router, global []http.Middleware) error {
	protected := false
	for _, m := range global {
		protected = protected || m.ID() == http.CSRFMiddlewareID
	}
	for _, route := range router.Routes() {
		a := route.Authentication
		if a == nil || a.Credential.Kind != http.CookieCredentialKind || a.Credential.OriginProtection || protected {
			continue
		}
		local := false
		for _, id := range route.Middlewares {
			local = local || id == http.CSRFMiddlewareID
		}
		if !local {
			return fault.New(fault.Invalid, "cookie-authenticated routes require CSRF protection")
		}
	}
	return nil
}
