package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/value"
)

// CSPMiddlewareID identifies the typed content-security-policy middleware.
const CSPMiddlewareID MiddlewareID = "foundry.content-security-policy"
const cspNonceSourceBytes = len("'nonce-'") + 44 // 32 random bytes in padded base64.

// CSPConfig permits independent enforced and report-only policies. Zero emits
// neither. Declarations are copied during middleware construction.
type CSPConfig struct {
	Enforce    value.Optional[CSPPolicy]
	ReportOnly value.Optional[CSPPolicy]
}

func (c CSPConfig) Validate() error { _, _, err := compileCSPConfig(c); return err }

func compileCSPConfig(c CSPConfig) (compiledCSP, compiledCSP, error) {
	var enforce, report compiledCSP
	var err error
	if p, ok := c.Enforce.Get(); ok {
		enforce, err = compileCSPPolicy(p, false)
		if err != nil {
			return compiledCSP{}, compiledCSP{}, err
		}
	}
	if p, ok := c.ReportOnly.Get(); ok {
		report, err = compileCSPPolicy(p, true)
		if err != nil {
			return compiledCSP{}, compiledCSP{}, err
		}
	}
	return enforce, report, nil
}

// CSPNonce is an opaque, request-scoped random value. String returns its base64
// value for an HTML nonce attribute; it is not pre-escaped or trusted markup.
type CSPNonce struct{ encoded string }

func (n CSPNonce) String() string { return n.encoded }

type cspNonceContextKey struct{}

// CSPNonceFromContext returns the value shared by the request's enforced and
// report-only policies, if either declares CSPNonceSource. Never copy this value
// into a cached page or use it to bless untrusted script/style content.
func CSPNonceFromContext(ctx context.Context) (CSPNonce, bool) {
	if ctx == nil {
		return CSPNonce{}, false
	}
	n, ok := ctx.Value(cspNonceContextKey{}).(CSPNonce)
	return n, ok && n.encoded != ""
}

// ContentSecurityPolicy sets typed headers before the next handler, preserving
// the native writer and streaming interfaces. A nonce policy generates 256 random
// bits per request, places the nonce in context, and sets Cache-Control: no-store.
// Trusted downstream code can override headers; keep the rendered nonce and CSP
// together. Static policies do not change cache headers. Apply globally to include
// router failures. Foundry does not rewrite HTML or automatically allow inline code.
// Nested policies within the same request reuse its nonce, so an added report-only
// policy cannot invalidate the nonce already emitted by an enforced outer policy.
func ContentSecurityPolicy(config CSPConfig) Middleware {
	enforce, report, err := compileCSPConfig(config)
	return defineReplayMiddleware(CSPMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var nonceSource string
			if enforce.nonce || report.nonce {
				// crypto/rand.Read fills the buffer and terminates the process on
				// entropy failure, as guaranteed by the supported Go toolchain.
				nonce, present := CSPNonceFromContext(r.Context())
				if !present {
					var bytes [32]byte
					rand.Read(bytes[:])
					nonce = CSPNonce{base64.StdEncoding.EncodeToString(bytes[:])}
				}
				nonceSource = "'nonce-" + nonce.encoded + "'"
				r = r.WithContext(context.WithValue(r.Context(), cspNonceContextKey{}, nonce))
				w.Header().Set("Cache-Control", "no-store")
			}
			if enforce.text != "" {
				w.Header().Set("Content-Security-Policy", strings.ReplaceAll(enforce.text, "\x00", nonceSource))
			}
			if report.text != "" {
				w.Header().Set("Content-Security-Policy-Report-Only", strings.ReplaceAll(report.text, "\x00", nonceSource))
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}
