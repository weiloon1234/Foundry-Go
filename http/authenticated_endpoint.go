package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AuthenticatedEndpoint preserves DTO contracts and supplies a concrete model
// only after its guard succeeds. Model fields never become response DTOs.
type AuthenticatedEndpoint[P, Q, B, M, R any] struct {
	endpoint      Endpoint[P, Q, B, R]
	authorization *func(context.Context, M, Input[P, Q, B]) error
	authenticationBinding[M]
}

// RequireAuthentication binds a Guarded transport declaration. Authentication
// runs before parameter/body decoding; its model is reused by the handler and
// policies within this request. Verified identity is attached to shared attribution
// before decoding, preserving request metadata. An unbound Guarded endpoint cannot register.
func RequireAuthentication[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], authentication *Authentication, guard auth.Guard[M]) AuthenticatedEndpoint[P, Q, B, M, R] {
	return AuthenticatedEndpoint[P, Q, B, M, R]{endpoint: endpoint, authenticationBinding: authenticationBinding[M]{authentication: authentication, guard: guard}}
}

// WithScopes requires the credential ceiling before input decoding.
func (e AuthenticatedEndpoint[P, Q, B, M, R]) WithScopes(required auth.AccessScopes[M]) AuthenticatedEndpoint[P, Q, B, M, R] {
	e.authenticationBinding = e.authenticationBinding.withScopes(required)
	return e
}

// WithPermissions requires every registered current-model capability before decoding.
func (e AuthenticatedEndpoint[P, Q, B, M, R]) WithPermissions(required ...auth.Permission[M]) AuthenticatedEndpoint[P, Q, B, M, R] {
	e.authenticationBinding = e.authenticationBinding.withPermissions(required...)
	return e
}
func (e AuthenticatedEndpoint[P, Q, B, M, R]) Validate() error { return e.validate(false) }
func (e AuthenticatedEndpoint[P, Q, B, M, R]) validate(optional bool) error {
	if e.authorization != nil && *e.authorization == nil {
		return fault.New(fault.Invalid, "request authorization callback is missing")
	}
	if err := e.authenticationBinding.validate(e.endpoint.route.spec.Access, optional); err != nil {
		return err
	}
	return e.endpoint.Validate()
}
func (e AuthenticatedEndpoint[P, Q, B, M, R]) bound(optional bool) Endpoint[P, Q, B, R] {
	endpoint := e.endpoint
	endpoint.route.authentication = e.authenticationBinding.info(optional)
	return endpoint.WithMiddleware(e.authenticationBinding.chain(optional)...)
}
func (e AuthenticatedEndpoint[P, Q, B, M, R]) Description() (EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	return e.bound(false).Description()
}
func (e AuthenticatedEndpoint[P, Q, B, M, R]) URL(ctx context.Context, path P, query Q) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.endpoint.URL(ctx, path, query)
}
func (e AuthenticatedEndpoint[P, Q, B, M, R]) Handle(handler func(context.Context, M, Input[P, Q, B]) (R, error)) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated endpoint requires a handler"))
	}
	return e.authorized().Handle(func(ctx context.Context, in Input[P, Q, B]) (R, error) {
		subject, err := e.guard.Require(ctx)
		if err != nil {
			return *new(R), authenticationError(err)
		}
		result, err := handler(ctx, subject, in)
		if err != nil {
			return *new(R), authenticationError(err)
		}
		return result, nil
	})
}

// HandleBound runs bind with the concrete actor after request (and actor)
// authorization and before validation; see Binding.
func (e AuthenticatedEndpoint[P, Q, B, M, R]) HandleBound(bind AuthenticatedBinding[P, Q, B, M, R]) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if bind == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated endpoint requires a binding"))
	}
	return e.authorized().HandleBound(bindSubject(bind, e.guard.Require))
}

// authorized installs actor authorization as the endpoint's request
// authorization stage, so it runs before validation (FormRequest order).
func (e AuthenticatedEndpoint[P, Q, B, M, R]) authorized() Endpoint[P, Q, B, R] {
	endpoint := e.bound(false)
	if e.authorization == nil {
		return endpoint
	}
	authorize := *e.authorization
	return endpoint.appendAuthorization(func(ctx context.Context, in Input[P, Q, B]) error {
		subject, err := e.guard.Require(ctx)
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

func OptionalAuthentication[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], authentication *Authentication, guard auth.Guard[M]) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	return OptionalAuthenticationEndpoint[P, Q, B, M, R]{required: RequireAuthentication(endpoint, authentication, guard)}
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) Validate() error {
	if e.authorization != nil && *e.authorization == nil {
		return fault.New(fault.Invalid, "request authorization callback is missing")
	}
	return e.required.validate(true)
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) Description() (EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	return e.required.bound(true).Description()
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) URL(ctx context.Context, path P, query Q) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.required.endpoint.URL(ctx, path, query)
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) Handle(handler func(context.Context, value.Optional[M], Input[P, Q, B]) (R, error)) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "optional authentication endpoint requires a handler"))
	}
	return e.authorized().Handle(func(ctx context.Context, in Input[P, Q, B]) (R, error) {
		subject, err := e.required.guard.Optional(ctx)
		if err != nil {
			return *new(R), authenticationError(err)
		}
		result, err := handler(ctx, subject, in)
		if err != nil {
			return *new(R), authenticationError(err)
		}
		return result, nil
	})
}

// HandleBound runs bind with the optional actor after request (and actor)
// authorization and before validation; see Binding.
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) HandleBound(bind AuthenticatedBinding[P, Q, B, value.Optional[M], R]) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if bind == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "optional authentication endpoint requires a binding"))
	}
	return e.authorized().HandleBound(bindSubject(bind, e.required.guard.Optional))
}

// authorized installs optional-actor authorization as the endpoint's request
// authorization stage, before validation.
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) authorized() Endpoint[P, Q, B, R] {
	endpoint := e.required.bound(true)
	if e.authorization == nil {
		return endpoint
	}
	authorize := *e.authorization
	return endpoint.appendAuthorization(func(ctx context.Context, in Input[P, Q, B]) error {
		subject, err := e.required.guard.Optional(ctx)
		if err != nil {
			return authenticationError(err)
		}
		if err := authorize(ctx, subject, in); err != nil {
			return authenticationError(err)
		}
		return nil
	})
}
