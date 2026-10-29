package http

import (
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AuthenticatedRoute supplies a concrete verified model while retaining native
// transport capabilities. Raw request/response payloads remain explicit boundaries.
// Configure path, middleware and route scope before adding authentication.
type AuthenticatedRoute[P, M any] struct {
	route   Route[P]
	binding authenticationBinding[M]
}

func RequireRouteAuthentication[P, M any](route Route[P], authentication *Authentication, guard auth.Guard[M], options ...GuardOption[M]) AuthenticatedRoute[P, M] {
	return ConfigureAuthenticatedRoute(AuthenticatedRoute[P, M]{route: route, binding: authenticationBinding[M]{authentication: authentication, guard: guard}}, options...)
}
func ValidateAuthenticatedRoute[P, M any](r AuthenticatedRoute[P, M]) error {
	return authenticatedRouteValidate(r, false)
}
func authenticatedRouteValidate[P, M any](r AuthenticatedRoute[P, M], optional bool) error {
	if err := authenticationValidate(r.binding, r.route.spec.Access, optional); err != nil {
		return err
	}
	return ValidateRoute(r.route)
}
func authenticatedRouteBound[P, M any](r AuthenticatedRoute[P, M], optional bool) Route[P] {
	route := r.route
	route.authentication = authenticationInfo(r.binding, optional)
	return ConfigureRoute(route, Use(authenticationChain(r.binding, optional)...))
}
func authenticatedRouteDescription[P, M any](r AuthenticatedRoute[P, M], optional bool) (RouteInfo, error) {
	if err := authenticatedRouteValidate(r, optional); err != nil {
		return RouteInfo{}, err
	}
	bound := authenticatedRouteBound(r, optional)
	segments, err := routeValidate(bound)
	if err != nil {
		return RouteInfo{}, err
	}
	return routeInfo(bound, segments, true), nil
}
func InspectAuthenticatedRoute[P, M any](r AuthenticatedRoute[P, M]) (RouteInfo, error) {
	return authenticatedRouteDescription(r, false)
}
func AuthenticatedRouteURL[P, M any](r AuthenticatedRoute[P, M], path P) (string, error) {
	if err := ValidateAuthenticatedRoute(r); err != nil {
		return "", err
	}
	return RouteURL(r.route, path)
}

// HandleAuthenticatedRaw admits the guard and requirements before path decoding. The original
// writer, URL, RequestURI, headers and body remain available to the native handler.
func HandleAuthenticatedRaw[P, M any](r AuthenticatedRoute[P, M], handler func(stdhttp.ResponseWriter, *stdhttp.Request, M, P)) RouteRegistration {
	if err := ValidateAuthenticatedRoute(r); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "authenticated raw route requires a handler"))
	}
	return HandleRaw(authenticatedRouteBound(r, false), func(w stdhttp.ResponseWriter, request *stdhttp.Request, path P) {
		subject, err := r.binding.guard.Require(request.Context())
		if err != nil {
			r.binding.authentication.writeFailure(w, request, r.binding.guard.Source(), err)
			return
		}
		handler(w, request, subject, path)
	})
}

// OptionalAuthenticationRoute distinguishes absent credentials from failed
// credentials with value.Optional[M]. Only absent credentials reach it anonymously.
type OptionalAuthenticationRoute[P, M any] struct{ required AuthenticatedRoute[P, M] }

func OptionalRouteAuthentication[P, M any](route Route[P], authentication *Authentication, guard auth.Guard[M], options ...ActorOption[M]) OptionalAuthenticationRoute[P, M] {
	return ConfigureOptionalRoute(OptionalAuthenticationRoute[P, M]{required: RequireRouteAuthentication(route, authentication, guard)}, options...)
}
func ValidateOptionalRoute[P, M any](r OptionalAuthenticationRoute[P, M]) error {
	return authenticatedRouteValidate(r.required, true)
}
func InspectOptionalRoute[P, M any](r OptionalAuthenticationRoute[P, M]) (RouteInfo, error) {
	return authenticatedRouteDescription(r.required, true)
}
func OptionalRouteURL[P, M any](r OptionalAuthenticationRoute[P, M], path P) (string, error) {
	if err := ValidateOptionalRoute(r); err != nil {
		return "", err
	}
	return RouteURL(r.required.route, path)
}
func HandleOptionalRaw[P, M any](r OptionalAuthenticationRoute[P, M], handler func(stdhttp.ResponseWriter, *stdhttp.Request, value.Optional[M], P)) RouteRegistration {
	if err := ValidateOptionalRoute(r); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "optional raw route requires a handler"))
	}
	return HandleRaw(authenticatedRouteBound(r.required, true), func(w stdhttp.ResponseWriter, request *stdhttp.Request, path P) {
		subject, err := r.required.binding.guard.Optional(request.Context())
		if err != nil {
			r.required.binding.authentication.writeFailure(w, request, r.required.binding.guard.Source(), err)
			return
		}
		handler(w, request, subject, path)
	})
}
