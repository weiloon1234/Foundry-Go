package http

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// SignedEndpoint retains the original path, query, body and response types.
// Configure scopes, limits, validation and middleware before calling Signed.
// Expiry/signature are framework transport fields, never fields of a domain DTO.
type SignedEndpoint[P, Q, B, R any] struct {
	endpoint Endpoint[P, Q, B, R]
	signer   URLSigner
	err      error
}

// Signed returns a signed endpoint preserving the original path, query, body and response types.
// Configure scopes, limits, validation and middleware before calling Signed.
// Expiry/signature are framework transport fields, never fields of a domain DTO.
func Signed[P, Q, B, R any](endpoint Endpoint[P, Q, B, R], signer URLSigner) SignedEndpoint[P, Q, B, R] {
	return SignedEndpoint[P, Q, B, R]{endpoint: endpoint, signer: signer}
}
func SignedEndpointID[P, Q, B, R any](e SignedEndpoint[P, Q, B, R]) RouteID {
	return EndpointID(e.endpoint)
}
func SignedEndpointMethod[P, Q, B, R any](e SignedEndpoint[P, Q, B, R]) Method {
	return EndpointMethod(e.endpoint)
}
func SignedEndpointPattern[P, Q, B, R any](e SignedEndpoint[P, Q, B, R]) string {
	return EndpointPattern(e.endpoint)
}
func ValidateSigned[P, Q, B, R any](e SignedEndpoint[P, Q, B, R]) error {
	if e.err != nil {
		return e.err
	}
	if err := ValidateEndpoint(e.endpoint); err != nil {
		return err
	}
	if err := e.signer.Validate(); err != nil {
		return err
	}
	for _, parameter := range e.endpoint.query.parameters {
		if parameter.info.Name == signedURLExpires || parameter.info.Name == signedURLSignature {
			return safeerror.NewFault(fault.Invalid, "signed endpoint query uses a reserved signing parameter")
		}
		if e.signer.policy.ignored[parameter.info.Name] {
			return safeerror.NewFault(fault.Invalid, "signed endpoint query declares a parameter its verification ignores")
		}
	}
	return nil
}

// WithPermanentLinks explicitly permits links without expiry; see SignedRoute.
func signedEndpointWithPermanentLinks[P, Q, B, R any](e SignedEndpoint[P, Q, B, R]) SignedEndpoint[P, Q, B, R] {
	e.signer.policy.permanent = true
	return e
}

// WithIgnoredParameters declares unauthenticated query parameters removed before
// verification. They cannot collide with the endpoint's declared query fields.
func signedEndpointWithIgnoredParameters[P, Q, B, R any](e SignedEndpoint[P, Q, B, R], names ...string) SignedEndpoint[P, Q, B, R] {
	policy, err := e.signer.policy.withIgnored(names)
	if err != nil && e.err == nil {
		e.err = err
	}
	e.signer.policy = policy
	return e
}

// PermanentEndpointURL signs a link without expiry; the endpoint must opt in.
func PermanentEndpointURL[P, Q, B, R any](e SignedEndpoint[P, Q, B, R], ctx context.Context, origin Origin, path P, query Q) (string, error) {
	return signedEndpointLink(e, ctx, origin, path, query, time.Time{}, signedURLForm{permanent: true})
}

// RelativeEndpointURL signs an origin-relative link, valid on every admitted origin.
func RelativeEndpointURL[P, Q, B, R any](e SignedEndpoint[P, Q, B, R], ctx context.Context, path P, query Q, expires time.Time) (string, error) {
	return signedEndpointLink(e, ctx, "", path, query, expires, signedURLForm{relative: true})
}
func (e SignedEndpoint[P, Q, B, R]) URL(ctx context.Context, origin Origin, path P, query Q, expires time.Time) (string, error) {
	return signedEndpointLink(e, ctx, origin, path, query, expires, signedURLForm{})
}

func signedEndpointLink[P, Q, B, R any](e SignedEndpoint[P, Q, B, R], ctx context.Context, origin Origin, path P, query Q, expires time.Time, form signedURLForm) (string, error) {
	if err := ValidateSigned(e); err != nil {
		return "", err
	}
	if err := e.signer.prepare(ctx); err != nil {
		return "", err
	}
	relative, err := e.endpoint.URL(ctx, path, query)
	if err != nil {
		return "", err
	}
	return e.signer.sealForm(ctx, e.endpoint.route.spec, EndpointPattern(e.endpoint), origin, relative, expires, form)
}

// Handle verifies the URL before any path/query/body codec, validation or domain
// handler. The existing endpoint decoder receives only its original typed query.
func (e SignedEndpoint[P, Q, B, R]) Handle(handler Handler[P, Q, B, R]) RouteRegistration {
	if err := ValidateSigned(e); err != nil {
		return RouteRegistration{err: err}
	}
	return signedRegistration(e.endpoint.Handle(handler), e.signer, e.endpoint.route.spec, EndpointPattern(e.endpoint), true)
}

// HandleSignedBound verifies the URL like Handle, then runs the binding stage.
func HandleSignedBound[P, Q, B, R any](e SignedEndpoint[P, Q, B, R], bind Binding[P, Q, B, R]) RouteRegistration {
	if err := ValidateSigned(e); err != nil {
		return RouteRegistration{err: err}
	}
	return signedRegistration(HandleBound(e.endpoint, bind), e.signer, e.endpoint.route.spec, EndpointPattern(e.endpoint), true)
}
func InspectSigned[P, Q, B, R any](e SignedEndpoint[P, Q, B, R]) (EndpointInfo, error) {
	if err := ValidateSigned(e); err != nil {
		return EndpointInfo{}, err
	}
	info, err := InspectEndpoint(e.endpoint)
	if err != nil {
		return EndpointInfo{}, err
	}
	info.Route.SignedURL = signedURLInfo(e.signer.policy)
	return info, nil
}
