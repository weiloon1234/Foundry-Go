package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

type HandlerTransport[P, Q, B, R any] interface {
	Validate() error
	Description() (EndpointInfo, error)
	Handle(Handler[P, Q, B, R]) RouteRegistration
}

// BindingTransport exposes the explicit pre-validation binding stage.
type BindingTransport[P, Q, B, R any] struct {
	validate func() error
	describe func() (EndpointInfo, error)
	bind     func(Binding[P, Q, B, R]) RouteRegistration
}

func (t BindingTransport[P, Q, B, R]) Validate() error {
	if t.validate == nil || t.describe == nil || t.bind == nil {
		return safeerror.NewFault(fault.Invalid, "binding transport is not defined")
	}
	return t.validate()
}
func (t BindingTransport[P, Q, B, R]) Description() (EndpointInfo, error) {
	if err := t.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	return t.describe()
}
func (t BindingTransport[P, Q, B, R]) HandleBound(bind Binding[P, Q, B, R]) RouteRegistration {
	if err := t.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if bind == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "endpoint requires a binding"))
	}
	return t.bind(bind)
}

// AuthenticatedBindingTransport exposes the explicit pre-validation binding stage.
type AuthenticatedBindingTransport[P, Q, B, S, R any] struct {
	validate func() error
	describe func() (EndpointInfo, error)
	bind     func(AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration
}

func (t AuthenticatedBindingTransport[P, Q, B, S, R]) Validate() error {
	if t.validate == nil || t.describe == nil || t.bind == nil {
		return safeerror.NewFault(fault.Invalid, "binding transport is not defined")
	}
	return t.validate()
}
func (t AuthenticatedBindingTransport[P, Q, B, S, R]) Description() (EndpointInfo, error) {
	if err := t.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	return t.describe()
}
func (t AuthenticatedBindingTransport[P, Q, B, S, R]) HandleBound(bind AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration {
	if err := t.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if bind == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "endpoint requires a binding"))
	}
	return t.bind(bind)
}
func ForBinding[P, Q, B, R any](endpoint Endpoint[P, Q, B, R]) BindingTransport[P, Q, B, R] {
	return BindingTransport[P, Q, B, R]{
		validate: func() error { return ValidateEndpoint(endpoint) },
		describe: func() (EndpointInfo, error) { return InspectEndpoint(endpoint) },
		bind:     func(bind Binding[P, Q, B, R]) RouteRegistration { return HandleBound(endpoint, bind) },
	}
}
func ForSignedBinding[P, Q, B, R any](endpoint SignedEndpoint[P, Q, B, R]) BindingTransport[P, Q, B, R] {
	return BindingTransport[P, Q, B, R]{
		validate: func() error { return ValidateSigned(endpoint) },
		describe: func() (EndpointInfo, error) { return InspectSigned(endpoint) },
		bind:     func(bind Binding[P, Q, B, R]) RouteRegistration { return HandleSignedBound(endpoint, bind) },
	}
}
func ForAuthenticatedBinding[P, Q, B, S, R any](endpoint AuthenticatedEndpoint[P, Q, B, S, R]) AuthenticatedBindingTransport[P, Q, B, S, R] {
	return AuthenticatedBindingTransport[P, Q, B, S, R]{
		validate: func() error { return ValidateAuthenticated(endpoint) },
		describe: func() (EndpointInfo, error) { return InspectAuthenticated(endpoint) },
		bind: func(bind AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration {
			return HandleAuthenticatedBound(endpoint, bind)
		},
	}
}
func ForOptionalBinding[P, Q, B, S, R any](endpoint OptionalAuthenticationEndpoint[P, Q, B, S, R]) AuthenticatedBindingTransport[P, Q, B, value.Optional[S], R] {
	return AuthenticatedBindingTransport[P, Q, B, value.Optional[S], R]{
		validate: func() error { return ValidateOptional(endpoint) },
		describe: func() (EndpointInfo, error) { return InspectOptional(endpoint) },
		bind: func(bind AuthenticatedBinding[P, Q, B, value.Optional[S], R]) RouteRegistration {
			return HandleOptionalBound(endpoint, bind)
		},
	}
}
func ForSignedAuthenticatedBinding[P, Q, B, S, R any](endpoint SignedAuthenticatedEndpoint[P, Q, B, S, R]) AuthenticatedBindingTransport[P, Q, B, S, R] {
	return AuthenticatedBindingTransport[P, Q, B, S, R]{
		validate: func() error { return ValidateSignedAuthenticated(endpoint) },
		describe: func() (EndpointInfo, error) { return InspectSignedAuthenticated(endpoint) },
		bind: func(bind AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration {
			return HandleSignedAuthenticatedBound(endpoint, bind)
		},
	}
}
func ForSignedOptionalBinding[P, Q, B, S, R any](endpoint SignedAuthenticatedEndpoint[P, Q, B, value.Optional[S], R]) AuthenticatedBindingTransport[P, Q, B, value.Optional[S], R] {
	return ForSignedAuthenticatedBinding(endpoint)
}
func AfterValidation[P, Q, B, R any](transport HandlerTransport[P, Q, B, R]) BindingTransport[P, Q, B, R] {
	if credential.IsNil(transport) {
		return BindingTransport[P, Q, B, R]{}
	}
	return BindingTransport[P, Q, B, R]{
		validate: transport.Validate, describe: transport.Description, bind: func(bind Binding[P, Q, B, R]) RouteRegistration {
			return transport.Handle(func(ctx context.Context, input Input[P, Q, B]) (R, error) {
				next, err := bind(ctx, input)
				if err != nil {
					return *new(R), err
				}
				if next == nil {
					return *new(R), InternalError.WithCause(safeerror.NewFault(fault.Internal, "endpoint binding returned no handler"))
				}
				return next(ctx, input)
			})
		},
	}
}
func AfterAuthenticatedValidation[P, Q, B, S, R any](transport AuthenticatedHandlerTransport[P, Q, B, S, R]) AuthenticatedBindingTransport[P, Q, B, S, R] {
	if credential.IsNil(transport) {
		return AuthenticatedBindingTransport[P, Q, B, S, R]{}
	}
	return AuthenticatedBindingTransport[P, Q, B, S, R]{
		validate: transport.Validate, describe: transport.Description, bind: func(bind AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration {
			return transport.Handle(func(ctx context.Context, actor S, input Input[P, Q, B]) (R, error) {
				next, err := bind(ctx, actor, input)
				if err != nil {
					return *new(R), err
				}
				if next == nil {
					return *new(R), InternalError.WithCause(safeerror.NewFault(fault.Internal, "authenticated binding returned no handler"))
				}
				return next(ctx, actor, input)
			})
		},
	}
}
