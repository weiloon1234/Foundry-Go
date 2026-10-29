package http

import (
	"context"
	stdhttp "net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SignedAuthenticatedRoute adds URL signatures without discarding the raw
// handler's concrete required/optional subject or its native writer capabilities.
type SignedAuthenticatedRoute[P, S any] struct {
	validate func() error
	describe func() (RouteInfo, error)
	handle   func(func(stdhttp.ResponseWriter, *stdhttp.Request, S, P)) RouteRegistration
	signed   SignedRoute[P]
}

func SignAuthenticatedRoute[P, M any](r AuthenticatedRoute[P, M], signer URLSigner) SignedAuthenticatedRoute[P, M] {
	return SignedAuthenticatedRoute[P, M]{validate: func() error { return ValidateAuthenticatedRoute(r) }, describe: func() (RouteInfo, error) { return InspectAuthenticatedRoute(r) }, handle: func(handler func(stdhttp.ResponseWriter, *stdhttp.Request, M, P)) RouteRegistration {
		return HandleAuthenticatedRaw(r, handler)
	}, signed: SignRoute(r.route, signer)}
}
func SignOptionalRoute[P, M any](r OptionalAuthenticationRoute[P, M], signer URLSigner) SignedAuthenticatedRoute[P, value.Optional[M]] {
	return SignedAuthenticatedRoute[P, value.Optional[M]]{validate: func() error { return ValidateOptionalRoute(r) }, describe: func() (RouteInfo, error) { return InspectOptionalRoute(r) }, handle: func(handler func(stdhttp.ResponseWriter, *stdhttp.Request, value.Optional[M], P)) RouteRegistration {
		return HandleOptionalRaw(r, handler)
	}, signed: SignRoute(r.required.route, signer)}
}
func ValidateSignedAuthenticatedRoute[P, S any](r SignedAuthenticatedRoute[P, S]) error {
	if r.validate == nil || r.describe == nil || r.handle == nil {
		return safeerror.NewFault(fault.Invalid, "signed raw authentication requires a transport")
	}
	if err := r.validate(); err != nil {
		return err
	}
	return ValidateSignedRoute(r.signed)
}
func SignedAuthenticatedRouteURL[P, S any](r SignedAuthenticatedRoute[P, S], ctx context.Context, origin Origin, path P, expires time.Time) (string, error) {
	if err := ValidateSignedAuthenticatedRoute(r); err != nil {
		return "", err
	}
	return SignedRouteURL(r.signed, ctx, origin, path, expires)
}

// WithPermanentLinks explicitly permits links without expiry; see SignedRoute.
func signedAuthenticatedRouteWithPermanentLinks[P, S any](r SignedAuthenticatedRoute[P, S]) SignedAuthenticatedRoute[P, S] {
	r.signed = ConfigureSignedRoute(r.signed, PermanentLinks())
	return r
}

// WithIgnoredParameters declares unauthenticated query parameters; see SignedRoute.
func signedAuthenticatedRouteWithIgnoredParameters[P, S any](r SignedAuthenticatedRoute[P, S], names ...string) SignedAuthenticatedRoute[P, S] {
	r.signed = ConfigureSignedRoute(r.signed, IgnoredParameters(names...))
	return r
}
func PermanentAuthenticatedRouteURL[P, S any](r SignedAuthenticatedRoute[P, S], ctx context.Context, origin Origin, path P) (string, error) {
	if err := ValidateSignedAuthenticatedRoute(r); err != nil {
		return "", err
	}
	return PermanentRouteURL(r.signed, ctx, origin, path)
}
func RelativeAuthenticatedRouteURL[P, S any](r SignedAuthenticatedRoute[P, S], ctx context.Context, path P, expires time.Time) (string, error) {
	if err := ValidateSignedAuthenticatedRoute(r); err != nil {
		return "", err
	}
	return RelativeRouteURL(r.signed, ctx, path, expires)
}
func InspectSignedAuthenticatedRoute[P, S any](r SignedAuthenticatedRoute[P, S]) (RouteInfo, error) {
	if err := ValidateSignedAuthenticatedRoute(r); err != nil {
		return RouteInfo{}, err
	}
	info, err := r.describe()
	if err != nil {
		return RouteInfo{}, err
	}
	info.SignedURL = signedURLInfo(r.signed.signer.policy)
	return info, nil
}
func HandleSignedAuthenticatedRaw[P, S any](r SignedAuthenticatedRoute[P, S], handler func(stdhttp.ResponseWriter, *stdhttp.Request, S, P)) RouteRegistration {
	if err := ValidateSignedAuthenticatedRoute(r); err != nil {
		return InvalidRouteRegistration(err)
	}
	return signedRegistration(r.handle(handler), r.signed.signer, r.signed.route.spec, SignedRoutePattern(r.signed), false)
}
