package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AuthenticatedEndpoint preserves DTO contracts and supplies a concrete model
// only after its guard succeeds. Model fields never become response DTOs.
type AuthenticatedEndpoint[P, Q, B, M, R any] struct {
	endpoint      Endpoint[P, Q, B, R]
	authorization *func(context.Context, M, Input[P, Q, B]) error
	binding       authenticationBinding[M]
}

// RequireAuthentication binds a Guarded transport declaration. Authentication
// runs before parameter/body decoding; its model is reused by the handler and
// policies within this request. Verified identity is attached to shared attribution
// before decoding, preserving request metadata. An unbound Guarded endpoint cannot register.
func RequireAuthentication[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], authentication *Authentication, guard auth.Guard[M], options ...GuardOption[M]) AuthenticatedEndpoint[P, Q, B, M, R] {
	return ConfigureAuthenticated(AuthenticatedEndpoint[P, Q, B, M, R]{endpoint: endpoint, binding: authenticationBinding[M]{authentication: authentication, guard: guard}}, options...)
}

func ValidateAuthenticated[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R]) error {
	return authenticatedEndpointValidate(e, false)
}
func authenticatedEndpointValidate[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R], optional bool) error {
	if e.authorization != nil && *e.authorization == nil {
		return safeerror.NewFault(fault.Invalid, "request authorization callback is missing")
	}
	if err := authenticationValidate(e.binding, e.endpoint.route.spec.Access, optional); err != nil {
		return err
	}
	return ValidateEndpoint(e.endpoint)
}
func authenticatedEndpointBound[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R], optional bool) Endpoint[P, Q, B, R] {
	endpoint := e.endpoint
	endpoint.route.authentication = authenticationInfo(e.binding, optional)
	return ConfigureEndpoint(endpoint, Use(authenticationChain(e.binding, optional)...))
}
func InspectAuthenticated[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R]) (EndpointInfo, error) {
	if err := ValidateAuthenticated(e); err != nil {
		return EndpointInfo{}, err
	}
	return InspectEndpoint(authenticatedEndpointBound(e, false))
}
func (e AuthenticatedEndpoint[P, Q, B, M, R]) URL(ctx context.Context, path P, query Q) (string, error) {
	if err := ValidateAuthenticated(e); err != nil {
		return "", err
	}
	return e.endpoint.URL(ctx, path, query)
}
func (e AuthenticatedEndpoint[P, Q, B, M, R]) Handle(handler func(context.Context, M, Input[P, Q, B]) (R, error)) RouteRegistration {
	if err := ValidateAuthenticated(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "authenticated endpoint requires a handler"))
	}
	return authenticatedEndpointAuthorized(e).Handle(subjectHandler(handler, e.binding.guard.Require))
}

// HandleAuthenticatedBound runs bind with the concrete actor after request (and actor)
// authorization and before validation; see Binding.
func HandleAuthenticatedBound[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R], bind AuthenticatedBinding[P, Q, B, M, R]) RouteRegistration {
	if err := ValidateAuthenticated(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if bind == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "authenticated endpoint requires a binding"))
	}
	return HandleBound(authenticatedEndpointAuthorized(e), bindSubject(bind, e.binding.guard.Require))
}

// authorized installs actor authorization as the endpoint's request
// authorization stage, so it runs before validation (FormRequest order).
func authenticatedEndpointAuthorized[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R]) Endpoint[P, Q, B, R] {
	endpoint := authenticatedEndpointBound(e, false)
	if e.authorization == nil {
		return endpoint
	}
	authorize := *e.authorization
	return endpointAppendAuthorization(endpoint, func(ctx context.Context, in Input[P, Q, B]) error {
		subject, err := e.binding.guard.Require(ctx)
		if err != nil {
			return authenticationError(err)
		}
		if err := authorize(ctx, subject, in); err != nil {
			return authenticationError(err)
		}
		return nil
	})
}

// OptionalAuthenticationEndpoint retains an explicit Optional model. Only absent
// credentials permit an anonymous handler; invalid/pending credentials fail.
type OptionalAuthenticationEndpoint[P, Q, B, M, R any] struct {
	required      AuthenticatedEndpoint[P, Q, B, M, R]
	authorization *func(context.Context, value.Optional[M], Input[P, Q, B]) error
}

func OptionalAuthentication[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], authentication *Authentication, guard auth.Guard[M], options ...ActorOption[M]) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	return ConfigureOptional(OptionalAuthenticationEndpoint[P, Q, B, M, R]{required: RequireAuthentication(endpoint, authentication, guard)}, options...)
}
func ValidateOptional[P, Q, B, M, R any](e OptionalAuthenticationEndpoint[P, Q, B, M, R]) error {
	if e.authorization != nil && *e.authorization == nil {
		return safeerror.NewFault(fault.Invalid, "request authorization callback is missing")
	}
	return authenticatedEndpointValidate(e.required, true)
}
func InspectOptional[P, Q, B, M, R any](e OptionalAuthenticationEndpoint[P, Q, B, M, R]) (EndpointInfo, error) {
	if err := ValidateOptional(e); err != nil {
		return EndpointInfo{}, err
	}
	return InspectEndpoint(authenticatedEndpointBound(e.required, true))
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) URL(ctx context.Context, path P, query Q) (string, error) {
	if err := ValidateOptional(e); err != nil {
		return "", err
	}
	return e.required.endpoint.URL(ctx, path, query)
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) Handle(handler func(context.Context, value.Optional[M], Input[P, Q, B]) (R, error)) RouteRegistration {
	if err := ValidateOptional(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "optional authentication endpoint requires a handler"))
	}
	return optionalAuthenticationEndpointAuthorized(e).Handle(subjectHandler(handler, e.required.binding.guard.Optional))
}

// HandleOptionalBound runs bind with the optional actor after request (and actor)
// authorization and before validation; see Binding.
func HandleOptionalBound[P, Q, B, M, R any](e OptionalAuthenticationEndpoint[P, Q, B, M, R], bind AuthenticatedBinding[P, Q, B, value.Optional[M], R]) RouteRegistration {
	if err := ValidateOptional(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if bind == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "optional authentication endpoint requires a binding"))
	}
	return HandleBound(optionalAuthenticationEndpointAuthorized(e), bindSubject(bind, e.required.binding.guard.Optional))
}

// authorized installs optional-actor authorization as the endpoint's request
// authorization stage, before validation.
func optionalAuthenticationEndpointAuthorized[P, Q, B, M, R any](e OptionalAuthenticationEndpoint[P, Q, B, M, R]) Endpoint[P, Q, B, R] {
	endpoint := authenticatedEndpointBound(e.required, true)
	if e.authorization == nil {
		return endpoint
	}
	authorize := *e.authorization
	return endpointAppendAuthorization(endpoint, func(ctx context.Context, in Input[P, Q, B]) error {
		subject, err := e.required.binding.guard.Optional(ctx)
		if err != nil {
			return authenticationError(err)
		}
		if err := authorize(ctx, subject, in); err != nil {
			return authenticationError(err)
		}
		return nil
	})
}
