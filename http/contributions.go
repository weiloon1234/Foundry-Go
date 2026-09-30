package http

import (
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type routeDeclaration interface {
	ID() RouteID
	Validate() error
	contributionContract() contributionContract
}
type routerRegistration struct{}

func routerRegistrationKey(key foundation.Key[*Router]) foundation.Key[routerRegistration] {
	return foundation.NewKey[routerRegistration](fmt.Sprintf("foundry.http.router.%q", key.Name()))
}

func routeContributions(key foundation.Key[*Router]) foundation.Collection[RouteRegistration] {
	return foundation.NewCollection[RouteRegistration](fmt.Sprintf("http.routes.%q", key.Name()))
}
func middlewareContributions(key foundation.Key[*Router]) foundation.Collection[Middleware] {
	return foundation.NewCollection[Middleware](fmt.Sprintf("http.middleware.%q", key.Name()))
}

// spaDeclaration is one RegisterSPA contribution with its assets resolved.
type spaDeclaration struct {
	id     RouteID
	assets *Assets
	config SPAConfig
}

func spaContributions(key foundation.Key[*Router]) foundation.Collection[spaDeclaration] {
	return foundation.NewCollection[spaDeclaration](fmt.Sprintf("http.spas.%q", key.Name()))
}

// RegisterRoute preserves the concrete route/endpoint descriptor through the
// contribution boundary. Its factory uses the existing typed Handle methods;
// returning a different ID, method, path, access or Go wire type fails Build.
// Signing required by the descriptor must also be retained. Constructors can
// resolve domain services but must not resolve the router being constructed.
func RegisterRoute[D routeDeclaration](r *foundation.Registrar, key foundation.Key[*Router], declaration D, construct func(foundation.Resolver) (RouteRegistration, error)) error {
	if key.Name() == "" || construct == nil {
		return fault.New(fault.Invalid, "route contribution requires a router and constructor")
	}
	if err := declaration.Validate(); err != nil {
		return err
	}
	id := declaration.ID()
	contract := declaration.contributionContract()
	return foundation.ContributeAs[D](r, routeContributions(key), string(id), func(resolver foundation.Resolver) (RouteRegistration, error) {
		if _, err := foundation.Resolve(resolver, routerRegistrationKey(key)); err != nil {
			return RouteRegistration{}, err
		}
		registration, err := construct(resolver)
		if err != nil {
			return RouteRegistration{}, err
		}
		if registration.err != nil {
			return RouteRegistration{}, registration.err
		}
		if registration.info.ID != id || registration.handler == nil {
			return RouteRegistration{}, fault.New(fault.Invalid, "route contribution returned a different identity or no handler")
		}
		if !contract.accepts(registration.contract) {
			return RouteRegistration{}, fault.New(fault.Invalid, "route contribution returned a different transport contract")
		}
		return registration, nil
	})
}

// RegisterMiddleware adds outer middleware to each contributed route in this
// router, retaining its identity in route/endpoint inspection. Native unmatched
// redirects and 404/405 responses do not acquire a fabricated route identity.
func RegisterMiddleware(r *foundation.Registrar, key foundation.Key[*Router], middleware Middleware) error {
	if key.Name() == "" {
		return fault.New(fault.Invalid, "middleware contribution requires a router")
	}
	if err := middleware.Validate(); err != nil {
		return err
	}
	return foundation.Contribute(r, middlewareContributions(key), string(middleware.ID()), func(resolver foundation.Resolver) (Middleware, error) {
		if _, err := foundation.Resolve(resolver, routerRegistrationKey(key)); err != nil {
			return Middleware{}, err
		}
		return middleware, nil
	})
}

// RegisterSPA adds a SPA fallback, as Router.WithSPA declares it, to the router
// assembled under key. The route ID and configuration are checked here; assets
// is resolved when that router is built, during Build and before any provider
// boots, so an unknown assets key or a duplicate route ID or prefix fails Build
// before resources open. Asset mounts with a less specific or equal prefix
// defer to the SPA as WithSPA describes.
func RegisterSPA(r *foundation.Registrar, key foundation.Key[*Router], id RouteID, assets foundation.Key[*Assets], config SPAConfig) error {
	if key.Name() == "" || assets.Name() == "" || !identifier.Semantic(string(id)) {
		return fault.New(fault.Invalid, "SPA contribution requires a router, an assets key and a semantic route ID")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	config = config.snapshot()
	return foundation.Contribute(r, spaContributions(key), string(id), func(resolver foundation.Resolver) (spaDeclaration, error) {
		if _, err := foundation.Resolve(resolver, routerRegistrationKey(key)); err != nil {
			return spaDeclaration{}, err
		}
		source, err := foundation.Resolve(resolver, assets)
		if err != nil {
			return spaDeclaration{}, err
		}
		return spaDeclaration{id: id, assets: source, config: config}, nil
	})
}

// RegisterRouter assembles all contributions through the existing native router.
// An application may register this in its HTTP provider and resolve it from the
// HTTP Module handler factory. Assembly performs no network I/O.
func RegisterRouter(r *foundation.Registrar, key foundation.Key[*Router]) error {
	return registerRouter(r, key, nil)
}

// RegisterRouterWithRoutes combines ordinary typed handler registrations with
// provider/plugin contributions. Duplicate/ambiguous routes fail as one graph.
func RegisterRouterWithRoutes(r *foundation.Registrar, key foundation.Key[*Router], construct func(foundation.Resolver) ([]RouteRegistration, error)) error {
	if construct == nil {
		return fault.New(fault.Invalid, "router requires a route constructor")
	}
	return registerRouter(r, key, construct)
}
func registerRouter(r *foundation.Registrar, key foundation.Key[*Router], construct func(foundation.Resolver) ([]RouteRegistration, error)) error {
	if err := foundation.Provide(r, routerRegistrationKey(key), routerRegistration{}); err != nil {
		return err
	}
	return foundation.Factory(r, key, func(resolver foundation.Resolver) (*Router, error) {
		registrations, err := foundation.Contributions(resolver, routeContributions(key))
		if err != nil {
			return nil, err
		}
		if construct != nil {
			additional, err := construct(resolver)
			if err != nil {
				return nil, err
			}
			registrations = append(registrations, additional...)
		}
		middlewares, err := foundation.Contributions(resolver, middlewareContributions(key))
		if err != nil {
			return nil, err
		}
		for i := range registrations {
			item := &registrations[i]
			item.middlewares, err = appendMiddlewares(middlewares, item.middlewares)
			if err != nil {
				return nil, err
			}
			item.info = item.info.clone()
			item.info.Middlewares = middlewareIDs(item.middlewares)
			if previous := item.endpoint; previous != nil {
				ids := slices.Clone(item.info.Middlewares)
				item.endpoint = func() EndpointInfo { info := previous(); info.Route.Middlewares = slices.Clone(ids); return info }
			}
		}
		router, err := NewRouter(registrations...)
		if err != nil {
			return nil, err
		}
		spas, err := foundation.Contributions(resolver, spaContributions(key))
		if err != nil {
			return nil, err
		}
		for _, spa := range spas {
			if router, err = router.WithSPA(spa.id, spa.assets, spa.config); err != nil {
				return nil, err
			}
		}
		return router, nil
	})
}
