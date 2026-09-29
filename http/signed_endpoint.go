package http

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// SignedEndpoint retains the original path, query, body and response types.
// Configure scopes, limits, validation and middleware before calling Signed.
// Expiry/signature are framework transport fields, never fields of a domain DTO.
type SignedEndpoint[P, Q, B, R any] struct {
	endpoint Endpoint[P, Q, B, R]
	signer   URLSigner
	err      error
}

func (e Endpoint[P, Q, B, R]) Signed(signer URLSigner) SignedEndpoint[P, Q, B, R] {
	return SignedEndpoint[P, Q, B, R]{endpoint: e, signer: signer}
}
func (e SignedEndpoint[P, Q, B, R]) ID() RouteID     { return e.endpoint.ID() }
func (e SignedEndpoint[P, Q, B, R]) Method() Method  { return e.endpoint.Method() }
func (e SignedEndpoint[P, Q, B, R]) Pattern() string { return e.endpoint.Pattern() }
func (e SignedEndpoint[P, Q, B, R]) Validate() error {
	if e.err != nil {
		return e.err
	}
	if err := e.endpoint.Validate(); err != nil {
		return err
	}
	if err := e.signer.Validate(); err != nil {
		return err
	}
	for _, parameter := range e.endpoint.query.parameters {
		if parameter.info.Name == signedURLExpires || parameter.info.Name == signedURLSignature {
			return fault.New(fault.Invalid, "signed endpoint query uses a reserved signing parameter")
		}
		if e.signer.policy.ignored[parameter.info.Name] {
			return fault.New(fault.Invalid, "signed endpoint query declares a parameter its verification ignores")
		}
	}
	return nil
}

// WithPermanentLinks explicitly permits links without expiry; see SignedRoute.
func (e SignedEndpoint[P, Q, B, R]) WithPermanentLinks() SignedEndpoint[P, Q, B, R] {
	e.signer.policy.permanent = true
	return e
}

// WithIgnoredParameters declares unauthenticated query parameters removed before
// verification. They cannot collide with the endpoint's declared query fields.
func (e SignedEndpoint[P, Q, B, R]) WithIgnoredParameters(names ...string) SignedEndpoint[P, Q, B, R] {
	policy, err := e.signer.policy.withIgnored(names)
	if err != nil && e.err == nil {
		e.err = err
	}
	e.signer.policy = policy
	return e
}

// PermanentURL signs a link without expiry; the endpoint must opt in.
func (e SignedEndpoint[P, Q, B, R]) PermanentURL(ctx context.Context, origin Origin, path P, query Q) (string, error) {
	return e.link(ctx, origin, path, query, time.Time{}, signedURLForm{permanent: true})
}

// RelativeURL signs an origin-relative link, valid on every admitted origin.
func (e SignedEndpoint[P, Q, B, R]) RelativeURL(ctx context.Context, path P, query Q, expires time.Time) (string, error) {
	return e.link(ctx, "", path, query, expires, signedURLForm{relative: true})
}
func (e SignedEndpoint[P, Q, B, R]) URL(ctx context.Context, origin Origin, path P, query Q, expires time.Time) (string, error) {
	return e.link(ctx, origin, path, query, expires, signedURLForm{})
}

func (e SignedEndpoint[P, Q, B, R]) link(ctx context.Context, origin Origin, path P, query Q, expires time.Time, form signedURLForm) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	if err := e.signer.prepare(ctx); err != nil {
		return "", err
	}
	relative, err := e.endpoint.URL(ctx, path, query)
	if err != nil {
		return "", err
	}
	return e.signer.sealForm(ctx, e.endpoint.route.spec, e.endpoint.Pattern(), origin, relative, expires, form)
}

// Handle verifies the URL before any path/query/body codec, validation or domain
// handler. The existing endpoint decoder receives only its original typed query.
func (e SignedEndpoint[P, Q, B, R]) Handle(handler Handler[P, Q, B, R]) RouteRegistration {
	if err := e.Validate(); err != nil {
		return RouteRegistration{err: err}
	}
	return signedRegistration(e.endpoint.Handle(handler), e.signer, e.endpoint.route.spec, e.endpoint.Pattern(), true)
}

// HandleBound verifies the URL like Handle, then runs the binding stage.
func (e SignedEndpoint[P, Q, B, R]) HandleBound(bind Binding[P, Q, B, R]) RouteRegistration {
	if err := e.Validate(); err != nil {
		return RouteRegistration{err: err}
	}
	return signedRegistration(e.endpoint.HandleBound(bind), e.signer, e.endpoint.route.spec, e.endpoint.Pattern(), true)
}
func (e SignedEndpoint[P, Q, B, R]) Description() (EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	info, err := e.endpoint.Description()
	if err != nil {
		return EndpointInfo{}, err
	}
	info.Route.SignedURL = signedURLInfo(e.signer.policy)
	return info, nil
}
