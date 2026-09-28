package http

import stdhttp "net/http"

// SecurityHeadersMiddlewareID identifies the built-in response-default policy.
const SecurityHeadersMiddlewareID MiddlewareID = "foundry.security-headers"

// SecurityHeaders snapshots configuration and supplies headers before routing
// or handler execution. Apply it globally to include router error responses.
// It preserves the native ResponseWriter and optional interfaces. Subsequent
// handlers may deliberately override these defaults; this is not a response
// filter that rewrites already-committed headers or buffers a streamed body.
func SecurityHeaders(config SecurityHeadersConfig) Middleware {
	policy, err := compileSecurityHeaders(config)
	return defineReplayMiddleware(SecurityHeadersMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			for _, header := range policy.headers {
				w.Header().Set(string(header.Name), string(header.Value))
			}
			if policy.writeHSTS && IsSecure(r) {
				w.Header().Set("Strict-Transport-Security", string(policy.hsts))
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}
