package http

import stdhttp "net/http"

const CredentialRequestsMiddlewareID MiddlewareID = "foundry.credential-requests"

// CredentialRequests protects credential submissions even when the response is
// empty or contains no issued secret. It requires POST and TLS (including an
// explicitly trusted proxy), sets no-store/no-cache before input decoding, and
// disables referrer disclosure. It does not replace CSRF, ingress rate limiting,
// credential verification or authorization. Browser routes also use CSRF.
func CredentialRequests() Middleware {
	return defineReplayMiddleware(CredentialRequestsMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if !checkCredentialRequest(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}

// Secret response adapters and explicit request protection share one boundary.
func checkCredentialRequest(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		writeRoutingError(w, r, MethodNotAllowed)
		return false
	}
	if !IsSecure(r) {
		writeRoutingError(w, r, BadRequest)
		return false
	}
	return true
}
