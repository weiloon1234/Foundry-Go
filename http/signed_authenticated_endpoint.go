package http

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SignedAuthenticatedEndpoint retains the concrete subject and DTO contracts.
// Configure authentication requirements before Signed. Both credential admission
// and signature validation run before input decoding; signatures grant no identity.
type SignedAuthenticatedEndpoint[P, Q, B, S, R any] struct {
	validate   func() error
	describe   func() (EndpointInfo, error)
	authorized func() Endpoint[P, Q, B, R]
	subject    func(context.Context) (S, error)
	optional   bool
	signed     SignedEndpoint[P, Q, B, R]
}

func SignedAuthenticated[P, Q, B, M, R any](endpoint AuthenticatedEndpoint[P, Q, B, M, R], signer URLSigner) SignedAuthenticatedEndpoint[P, Q, B, M, R] {
	return SignedAuthenticatedEndpoint[P, Q, B, M, R]{
		validate:   func() error { return ValidateAuthenticated(endpoint) },
		describe:   func() (EndpointInfo, error) { return InspectAuthenticated(endpoint) },
		authorized: func() Endpoint[P, Q, B, R] { return authenticatedEndpointAuthorized(endpoint) },
		subject:    endpoint.binding.guard.Require,
		signed:     Signed(endpoint.endpoint, signer),
	}
}

func SignedOptionalAuthentication[P, Q, B, M, R any](endpoint OptionalAuthenticationEndpoint[P, Q, B, M, R], signer URLSigner) SignedAuthenticatedEndpoint[P, Q, B, value.Optional[M], R] {
	return SignedAuthenticatedEndpoint[P, Q, B, value.Optional[M], R]{
		validate:   func() error { return ValidateOptional(endpoint) },
		describe:   func() (EndpointInfo, error) { return InspectOptional(endpoint) },
		authorized: func() Endpoint[P, Q, B, R] { return optionalAuthenticationEndpointAuthorized(endpoint) },
		subject:    endpoint.required.binding.guard.Optional,
		optional:   true,
		signed:     Signed(endpoint.required.endpoint, signer),
	}
}
func SignedAuthenticatedEndpointID[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R]) RouteID {
	return SignedEndpointID(e.signed)
}
func SignedAuthenticatedEndpointMethod[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R]) Method {
	return SignedEndpointMethod(e.signed)
}
func SignedAuthenticatedEndpointPattern[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R]) string {
	return SignedEndpointPattern(e.signed)
}
func ValidateSignedAuthenticated[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R]) error {
	if e.validate == nil || e.describe == nil || e.authorized == nil || e.subject == nil {
		return safeerror.NewFault(fault.Invalid, "signed authentication requires a transport")
	}
	if err := e.validate(); err != nil {
		return err
	}
	return ValidateSigned(e.signed)
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) URL(ctx context.Context, origin Origin, path P, query Q, expires time.Time) (string, error) {
	if err := ValidateSignedAuthenticated(e); err != nil {
		return "", err
	}
	return e.signed.URL(ctx, origin, path, query, expires)
}

// WithPermanentLinks explicitly permits links without expiry; see SignedRoute.
func signedAuthenticatedEndpointWithPermanentLinks[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R]) SignedAuthenticatedEndpoint[P, Q, B, S, R] {
	e.signed = ConfigureSigned(e.signed, PermanentLinks())
	return e
}

// WithIgnoredParameters declares unauthenticated query parameters; see SignedEndpoint.
func signedAuthenticatedEndpointWithIgnoredParameters[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R], names ...string) SignedAuthenticatedEndpoint[P, Q, B, S, R] {
	e.signed = ConfigureSigned(e.signed, IgnoredParameters(names...))
	return e
}
func PermanentAuthenticatedEndpointURL[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R], ctx context.Context, origin Origin, path P, query Q) (string, error) {
	if err := ValidateSignedAuthenticated(e); err != nil {
		return "", err
	}
	return PermanentEndpointURL(e.signed, ctx, origin, path, query)
}
func RelativeAuthenticatedEndpointURL[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R], ctx context.Context, path P, query Q, expires time.Time) (string, error) {
	if err := ValidateSignedAuthenticated(e); err != nil {
		return "", err
	}
	return RelativeEndpointURL(e.signed, ctx, path, query, expires)
}
func InspectSignedAuthenticated[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R]) (EndpointInfo, error) {
	if err := ValidateSignedAuthenticated(e); err != nil {
		return EndpointInfo{}, err
	}
	info, err := e.describe()
	if err != nil {
		return EndpointInfo{}, err
	}
	info.Route.SignedURL = signedURLInfo(e.signed.signer.policy)
	return info, nil
}

// HandleSignedAuthenticatedBound verifies the URL like Handle, then runs the binding stage. The
// wrapped transport must be a framework authenticated endpoint.
func HandleSignedAuthenticatedBound[P, Q, B, S, R any](e SignedAuthenticatedEndpoint[P, Q, B, S, R], bind AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration {
	if err := ValidateSignedAuthenticated(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if bind == nil {
		message := "authenticated endpoint requires a binding"
		if e.optional {
			message = "optional authentication endpoint requires a binding"
		}
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, message))
	}
	return signedRegistration(HandleBound(e.authorized(), bindSubject(bind, e.subject)), e.signed.signer, e.signed.endpoint.route.spec, SignedAuthenticatedEndpointPattern(e), true)
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) Handle(handler func(context.Context, S, Input[P, Q, B]) (R, error)) RouteRegistration {
	if err := ValidateSignedAuthenticated(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if handler == nil {
		message := "authenticated endpoint requires a handler"
		if e.optional {
			message = "optional authentication endpoint requires a handler"
		}
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, message))
	}
	registration := e.authorized().Handle(subjectHandler(handler, e.subject))
	return signedRegistration(registration, e.signed.signer, e.signed.endpoint.route.spec, SignedAuthenticatedEndpointPattern(e), true)
}
