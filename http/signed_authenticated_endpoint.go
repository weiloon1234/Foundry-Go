package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

// SignedAuthenticatedEndpoint retains the concrete subject and DTO contracts.
// Configure authentication requirements before Signed. Both credential admission
// and signature validation run before input decoding; signatures grant no identity.
type SignedAuthenticatedEndpoint[P, Q, B, S, R any] struct {
	transport AuthenticatedTransport[P, Q, B, S, R]
	signed    SignedEndpoint[P, Q, B, R]
}

func (e AuthenticatedEndpoint[P, Q, B, M, R]) Signed(signer URLSigner) SignedAuthenticatedEndpoint[P, Q, B, M, R] {
	return SignedAuthenticatedEndpoint[P, Q, B, M, R]{transport: e, signed: e.endpoint.Signed(signer)}
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) Signed(signer URLSigner) SignedAuthenticatedEndpoint[P, Q, B, value.Optional[M], R] {
	return SignedAuthenticatedEndpoint[P, Q, B, value.Optional[M], R]{transport: e, signed: e.required.endpoint.Signed(signer)}
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) ID() RouteID     { return e.signed.ID() }
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) Method() Method  { return e.signed.Method() }
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) Pattern() string { return e.signed.Pattern() }
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) Validate() error {
	if e.transport == nil {
		return fault.New(fault.Invalid, "signed authentication requires a transport")
	}
	if err := e.transport.Validate(); err != nil {
		return err
	}
	return e.signed.Validate()
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) URL(ctx context.Context, origin Origin, path P, query Q, expires time.Time) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.signed.URL(ctx, origin, path, query, expires)
}

// WithPermanentLinks explicitly permits links without expiry; see SignedRoute.
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) WithPermanentLinks() SignedAuthenticatedEndpoint[P, Q, B, S, R] {
	e.signed = e.signed.WithPermanentLinks()
	return e
}

// WithIgnoredParameters declares unauthenticated query parameters; see SignedEndpoint.
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) WithIgnoredParameters(names ...string) SignedAuthenticatedEndpoint[P, Q, B, S, R] {
	e.signed = e.signed.WithIgnoredParameters(names...)
	return e
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) PermanentURL(ctx context.Context, origin Origin, path P, query Q) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.signed.PermanentURL(ctx, origin, path, query)
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) RelativeURL(ctx context.Context, path P, query Q, expires time.Time) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.signed.RelativeURL(ctx, path, query, expires)
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) Description() (EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	info, err := e.transport.Description()
	if err != nil {
		return EndpointInfo{}, err
	}
	info.Route.SignedURL = signedURLInfo(e.signed.signer.policy)
	return info, nil
}

// HandleBound verifies the URL like Handle, then runs the binding stage. The
// wrapped transport must be a framework authenticated endpoint.
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) HandleBound(bind AuthenticatedBinding[P, Q, B, S, R]) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	bound, ok := e.transport.(boundAuthenticatedTransport[P, Q, B, S, R])
	if !ok {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "signed authenticated transport does not support binding"))
	}
	return signedRegistration(bound.HandleBound(bind), e.signed.signer, e.signed.endpoint.route.spec, e.Pattern(), true)
}
func (e SignedAuthenticatedEndpoint[P, Q, B, S, R]) Handle(handler func(context.Context, S, Input[P, Q, B]) (R, error)) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	return signedRegistration(e.transport.Handle(handler), e.signed.signer, e.signed.endpoint.route.spec, e.Pattern(), true)
}
