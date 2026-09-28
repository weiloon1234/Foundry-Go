package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// SignedURLInfo describes framework-owned query parameters separately from an
// endpoint's domain query. Inspection/export never exposes signing key material.
type SignedURLInfo struct {
	Version            string `json:"version"`
	Algorithm          string `json:"algorithm"`
	ExpiresParameter   string `json:"expires_parameter"`
	SignatureParameter string `json:"signature_parameter"`
}

func signedURLInfo() *SignedURLInfo {
	return &SignedURLInfo{Version: signedURLVersion, Algorithm: "HMAC-SHA256", ExpiresParameter: signedURLExpires, SignatureParameter: signedURLSignature}
}

// Validate checks serialized inspection metadata against the runtime signer.
// It exposes no keys and does not generate or verify a credential.
func (info SignedURLInfo) Validate() error {
	if info != *signedURLInfo() {
		return fault.New(fault.Invalid, "unsupported signed URL metadata")
	}
	return nil
}

// SignedRoute retains concrete path parameters for link generation and handlers.
// Apply scopes/middleware to the original route before calling Signed.
type SignedRoute[P any] struct {
	route  Route[P]
	signer URLSigner
}

func (r Route[P]) Signed(signer URLSigner) SignedRoute[P] {
	return SignedRoute[P]{route: r, signer: signer}
}
func (r SignedRoute[P]) ID() RouteID     { return r.route.ID() }
func (r SignedRoute[P]) Method() Method  { return r.route.Method() }
func (r SignedRoute[P]) Pattern() string { return r.route.Pattern() }
func (r SignedRoute[P]) Validate() error {
	if err := r.route.Validate(); err != nil {
		return err
	}
	if r.route.Method() == TRACE {
		return fault.New(fault.Invalid, "TRACE cannot carry a signed URL")
	}
	return r.signer.Validate()
}

// URL uses an explicitly approved origin in background kernels. Request handlers
// can obtain one through PublicOrigin after PublicURLs middleware. Expiry is
// mandatory, measured in whole Unix seconds, and must be in the future.
func (r SignedRoute[P]) URL(ctx context.Context, origin Origin, path P, expires time.Time) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	if err := r.signer.prepare(ctx); err != nil {
		return "", err
	}
	relative, err := r.route.URL(path)
	if err != nil {
		return "", err
	}
	return r.signer.seal(ctx, r.route.spec, r.route.Pattern(), origin, relative, expires)
}

// HandleRaw verifies before path decoding or handler execution. PublicURLs must
// run outside the router; TrustedProxy must precede it behind a trusted edge.
// Raw handlers retain the original native request and signing query parameters.
func (r SignedRoute[P]) HandleRaw(handler RawHandler[P]) RouteRegistration {
	if err := r.Validate(); err != nil {
		return RouteRegistration{err: err}
	}
	return signedRegistration(r.route.HandleRaw(handler), r.signer, r.route.spec, r.route.Pattern(), false)
}

type signedEndpointQueryKey struct{}

func signedRegistration(registration RouteRegistration, signer URLSigner, spec RouteSpec, pattern string, typed bool) RouteRegistration {
	if registration.err != nil {
		return registration
	}
	registration.info.SignedURL = signedURLInfo()
	registration.contract.signed = true
	next := registration.handler
	registration.handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if _, ok := PublicOrigin(r.Context()); !ok {
			writeRoutingError(w, r, InternalError.WithCause(fault.New(fault.Missing, "signed route requires PublicURLs middleware")))
			return
		}
		if r.URL == nil || Method(r.Method) != spec.Method && !(spec.Method == GET && r.Method == stdhttp.MethodHead) {
			writeRoutingError(w, r, Forbidden.WithCause(ErrInvalidSignedURL))
			return
		}
		// PublicURLs proves admission; authenticate the actual trusted origin,
		// not its optional canonical alias used when generating ordinary links.
		origin, err := requestOrigin(r)
		if err != nil {
			writeRoutingError(w, r, Forbidden.WithCause(ErrInvalidSignedURL))
			return
		}
		query, err := signer.open(r.Context(), spec, pattern, origin, r.URL.RequestURI())
		if err != nil {
			switch {
			case errors.Is(err, ErrInvalidSignedURL):
				err = Forbidden.WithCause(ErrInvalidSignedURL)
			case r.Context().Err() != nil:
				err = RequestTimeout.WithCause(r.Context().Err())
			default:
				err = InternalError.WithCause(err)
			}
			writeRoutingError(w, r, err)
			return
		}
		if typed {
			// Only the typed query decoder sees the verified application query.
			// Native URL/RequestURI, body, headers and writer remain unchanged.
			r = r.WithContext(context.WithValue(r.Context(), signedEndpointQueryKey{}, query))
		}
		next.ServeHTTP(w, r)
	})
	if snapshot := registration.endpoint; snapshot != nil {
		registration.endpoint = func() EndpointInfo {
			info := snapshot()
			info.Route.SignedURL = signedURLInfo()
			return info
		}
	}
	return registration
}
