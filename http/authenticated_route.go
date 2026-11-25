package http

import (
	stdhttp "net/http"

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

// RouteAuthenticated binds a raw route to the binding's guard and the access rule
// that says who may call it: AllOf, AnyOf or binding.AnyActor(). Authentication runs
// before path decoding.
//
// Example: http.RouteAuthenticated(downloadRoute, users, http.AllOf(http.Scopes(read)))
func RouteAuthenticated[P, M any](route Route[P], binding GuardBinding[M], rule AccessRule[M], options ...ActorOption[M]) AuthenticatedRoute[P, M] {
	required := binding.declaration().withAccess(rule)
	return ConfigureAuthenticatedRoute(AuthenticatedRoute[P, M]{route: route, binding: required}, options...)
}

// ValidateAuthenticatedRoute checks the raw route and required authentication binding.
func ValidateAuthenticatedRoute[P, M any](r AuthenticatedRoute[P, M]) error {
	return authenticatedRouteValidate(r, false)
}
func authenticatedRouteValidate[P, M any](r AuthenticatedRoute[P, M], optional bool) error {
	if err := authenticationValidate(r.binding, r.route.spec.Access, optional, RouteMethod(r.route), RoutePattern(r.route), RouteIDOf(r.route)); err != nil {
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

// InspectAuthenticatedRoute returns validated route metadata including required
// authentication.
func InspectAuthenticatedRoute[P, M any](r AuthenticatedRoute[P, M]) (RouteInfo, error) {
	return authenticatedRouteDescription(r, false)
}

// AuthenticatedRouteURL validates the authenticated declaration and formats its typed
// path.
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

// RouteOptionallyAuthenticated adds the binding's optional typed subject and actor
// options to a raw route.
func RouteOptionallyAuthenticated[P, M any](route Route[P], binding GuardBinding[M], options ...ActorOption[M]) OptionalAuthenticationRoute[P, M] {
	required := AuthenticatedRoute[P, M]{route: route, binding: binding.declaration()}
	return ConfigureOptionalRoute(OptionalAuthenticationRoute[P, M]{required: required}, options...)
}

// ValidateOptionalRoute checks the raw route and optional authentication binding.
func ValidateOptionalRoute[P, M any](r OptionalAuthenticationRoute[P, M]) error {
	return authenticatedRouteValidate(r.required, true)
}

// InspectOptionalRoute returns validated route metadata including optional
// authentication.
func InspectOptionalRoute[P, M any](r OptionalAuthenticationRoute[P, M]) (RouteInfo, error) {
	return authenticatedRouteDescription(r.required, true)
}

// OptionalRouteURL validates the optional-authentication declaration and formats its
// typed path.
func OptionalRouteURL[P, M any](r OptionalAuthenticationRoute[P, M], path P) (string, error) {
	if err := ValidateOptionalRoute(r); err != nil {
		return "", err
	}
	return RouteURL(r.required.route, path)
}

// HandleOptionalRaw registers a raw HTTP handler receiving the guard's optional subject
// and decoded path.
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
