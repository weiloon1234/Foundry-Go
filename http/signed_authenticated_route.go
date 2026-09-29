package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	stdhttp "net/http"
	"time"
)

type authenticatedRawTransport[P, S any] interface {
	Validate() error
	Description() (RouteInfo, error)
	HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, S, P)) RouteRegistration
}

// SignedAuthenticatedRoute adds URL signatures without discarding the raw
// handler's concrete required/optional subject or its native writer capabilities.
type SignedAuthenticatedRoute[P, S any] struct {
	transport authenticatedRawTransport[P, S]
	signed    SignedRoute[P]
}

func (r AuthenticatedRoute[P, M]) Signed(signer URLSigner) SignedAuthenticatedRoute[P, M] {
	return SignedAuthenticatedRoute[P, M]{transport: r, signed: r.route.Signed(signer)}
}
func (r OptionalAuthenticationRoute[P, M]) Signed(signer URLSigner) SignedAuthenticatedRoute[P, value.Optional[M]] {
	return SignedAuthenticatedRoute[P, value.Optional[M]]{transport: r, signed: r.required.route.Signed(signer)}
}
func (r SignedAuthenticatedRoute[P, S]) Validate() error {
	if r.transport == nil {
		return fault.New(fault.Invalid, "signed raw authentication requires a transport")
	}
	if err := r.transport.Validate(); err != nil {
		return err
	}
	return r.signed.Validate()
}
func (r SignedAuthenticatedRoute[P, S]) URL(ctx context.Context, origin Origin, path P, expires time.Time) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.signed.URL(ctx, origin, path, expires)
}

// WithPermanentLinks explicitly permits links without expiry; see SignedRoute.
func (r SignedAuthenticatedRoute[P, S]) WithPermanentLinks() SignedAuthenticatedRoute[P, S] {
	r.signed = r.signed.WithPermanentLinks()
	return r
}

// WithIgnoredParameters declares unauthenticated query parameters; see SignedRoute.
func (r SignedAuthenticatedRoute[P, S]) WithIgnoredParameters(names ...string) SignedAuthenticatedRoute[P, S] {
	r.signed = r.signed.WithIgnoredParameters(names...)
	return r
}
func (r SignedAuthenticatedRoute[P, S]) PermanentURL(ctx context.Context, origin Origin, path P) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.signed.PermanentURL(ctx, origin, path)
}
func (r SignedAuthenticatedRoute[P, S]) RelativeURL(ctx context.Context, path P, expires time.Time) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.signed.RelativeURL(ctx, path, expires)
}
func (r SignedAuthenticatedRoute[P, S]) Description() (RouteInfo, error) {
	if err := r.Validate(); err != nil {
		return RouteInfo{}, err
	}
	info, err := r.transport.Description()
	if err != nil {
		return RouteInfo{}, err
	}
	info.SignedURL = signedURLInfo(r.signed.signer.policy)
	return info, nil
}
func (r SignedAuthenticatedRoute[P, S]) HandleRaw(handler func(stdhttp.ResponseWriter, *stdhttp.Request, S, P)) RouteRegistration {
	if err := r.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	return signedRegistration(r.transport.HandleRaw(handler), r.signed.signer, r.signed.route.spec, r.signed.Pattern(), false)
}
