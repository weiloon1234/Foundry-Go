package http

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	stdhttp "net/http"
)

// AuthenticatedRoute supplies a concrete verified model while retaining native
// transport capabilities. Raw request/response payloads remain explicit boundaries.
// Configure path, middleware and route scope before adding authentication.
type AuthenticatedRoute[P, M any] struct {
	route Route[P]
	authenticationBinding[M]
}

func RequireRouteAuthentication[P, M any](route Route[P], authentication *Authentication, guard auth.Guard[M]) AuthenticatedRoute[P, M] {
	return AuthenticatedRoute[P, M]{route: route, authenticationBinding: authenticationBinding[M]{authentication: authentication, guard: guard}}
}
func (r AuthenticatedRoute[P, M]) WithScopes(required auth.AccessScopes[M]) AuthenticatedRoute[P, M] {
	r.authenticationBinding = r.authenticationBinding.withScopes(required)
	return r
}
func (r AuthenticatedRoute[P, M]) WithPermissions(required ...auth.Permission[M]) AuthenticatedRoute[P, M] {
	r.authenticationBinding = r.authenticationBinding.withPermissions(required...)
	return r
}
func (r AuthenticatedRoute[P, M]) Validate() error { return r.validate(false) }
func (r AuthenticatedRoute[P, M]) validate(optional bool) error {
	if err := r.authenticationBinding.validate(r.route.spec.Access, optional); err != nil {
		return err
	}
	return r.route.Validate()
}
func (r AuthenticatedRoute[P, M]) bound(optional bool) Route[P] {
	route := r.route
	route.authentication = r.authenticationBinding.info(optional)
	return route.WithMiddleware(r.authenticationBinding.chain(optional)...)
}
func (r AuthenticatedRoute[P, M]) description(optional bool) (RouteInfo, error) {
	if err := r.validate(optional); err != nil {
		return RouteInfo{}, err
	}
	bound := r.bound(optional)
	segments, err := bound.validate()
	if err != nil {
		return RouteInfo{}, err
	}
	return bound.info(segments, true), nil
}
func (r AuthenticatedRoute[P, M]) Description() (RouteInfo, error) { return r.description(false) }
func (r AuthenticatedRoute[P, M]) URL(path P) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.route.URL(path)
}

// HandleRaw admits the guard and requirements before path decoding. The original
// writer, URL, RequestURI, headers and body remain available to the native handler.
func (r AuthenticatedRoute[P, M]) HandleRaw(handler func(stdhttp.ResponseWriter, *stdhttp.Request, M, P)) RouteRegistration {
	if err := r.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated raw route requires a handler"))
	}
	return r.bound(false).HandleRaw(func(w stdhttp.ResponseWriter, request *stdhttp.Request, path P) {
		subject, err := r.guard.Require(request.Context())
		if err != nil {
			r.authentication.writeFailure(w, request, r.guard.Source(), err)
			return
		}
		handler(w, request, subject, path)
	})
}

// OptionalAuthenticationRoute distinguishes absent credentials from failed
// credentials with value.Optional[M]. Only absent credentials reach it anonymously.
type OptionalAuthenticationRoute[P, M any] struct{ required AuthenticatedRoute[P, M] }

func OptionalRouteAuthentication[P, M any](route Route[P], authentication *Authentication, guard auth.Guard[M]) OptionalAuthenticationRoute[P, M] {
	return OptionalAuthenticationRoute[P, M]{required: RequireRouteAuthentication(route, authentication, guard)}
}
func (r OptionalAuthenticationRoute[P, M]) Validate() error { return r.required.validate(true) }
func (r OptionalAuthenticationRoute[P, M]) Description() (RouteInfo, error) {
	return r.required.description(true)
}
func (r OptionalAuthenticationRoute[P, M]) URL(path P) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.required.route.URL(path)
}
func (r OptionalAuthenticationRoute[P, M]) HandleRaw(handler func(stdhttp.ResponseWriter, *stdhttp.Request, value.Optional[M], P)) RouteRegistration {
	if err := r.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "optional raw route requires a handler"))
	}
	return r.required.bound(true).HandleRaw(func(w stdhttp.ResponseWriter, request *stdhttp.Request, path P) {
		subject, err := r.required.guard.Optional(request.Context())
		if err != nil {
			r.required.authentication.writeFailure(w, request, r.required.guard.Source(), err)
			return
		}
		handler(w, request, subject, path)
	})
}
