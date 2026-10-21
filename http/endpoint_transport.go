package http

import (
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

type endpointTransport struct {
	responseKind        payloadKind
	responseStatus      int
	credentials         bool
	clearsRefreshCookie bool
	readsRefreshCookie  bool
	cookie              *RefreshCookie
	files               FileResponseLimits
	browserResponse     bool
}

func (t endpointTransport) preflight(w stdhttp.ResponseWriter, r *stdhttp.Request) (stdhttp.ResponseWriter, *stdhttp.Request, bool) {
	if t.responseKind == payloadEvents {
		r = withLastEventID(r)
	}
	ctx := r.Context()
	cookie := t.cookie
	if (t.credentials || cookie != nil) && !checkCredentialRequest(w, r) {
		return w, r, false
	}
	if cookie != nil {
		// Origin protection precedes reading, so a cross-site request can
		// neither use nor clear the cookie.
		if err := cookie.csrf.check(r); err != nil {
			writeRoutingError(w, r, err)
			return w, r, false
		}
		if t.readsRefreshCookie || t.clearsRefreshCookie {
			clear, err := cookie.clear()
			if err != nil {
				writeRoutingError(w, r, InternalError.WithCause(err))
				return w, r, false
			}
			w = &refreshClearingWriter{ResponseWriter: w, clear: clear}
		}
	}
	if err := bindBrowserResponse(ctx, t.browserResponse); err != nil {
		writeRoutingError(w, r, err)
		return w, r, false
	}
	if t.responseKind == payloadDownload {
		if err := t.files.checkRange(r.Header.Values("Range")); err != nil {
			writeRoutingError(w, r, err)
			return w, r, false
		}
	}
	return w, r, true
}

func (t endpointTransport) publish(w stdhttp.ResponseWriter, r *stdhttp.Request, data preparedResponse, returned, callbackErr error) {
	ctx := r.Context()
	if data.operational != nil {
		logRouteFailure(r, "HTTP idempotent outcome committed with operational failure", data.operational)
	}
	if callbackErr != nil {
		logRouteFailure(r, "HTTP endpoint handler failed", callbackErr)
		writeRoutingError(w, r, InternalError.WithCause(callbackErr))
		return
	}
	// Preparation owns classification of codec/file-source failures. In particular,
	// cancellation must not mask a panic already contained by that boundary.
	// A returned error is authoritative: a later deadline never replaces it.
	if returned != nil {
		writeRoutingError(w, r, returned)
		return
	}
	// The handler succeeded and its response is prepared. A deadline that
	// expired afterwards does not turn completed work into a timeout, but a
	// browser-session credential is never published after the request ended.
	// Wrappers outside the router now deliver it unless the client left.
	completeRoute(ctx)
	if err := publishBrowserSession(ctx, w, data.statusOr(t.responseStatus)); err != nil {
		writeRoutingError(w, r, authenticationError(err))
		return
	}
	if data.setCookie != "" {
		w.Header().Add("Set-Cookie", data.setCookie)
	}
	if t.clearsRefreshCookie {
		clear, err := t.cookie.clear()
		if err != nil {
			writeRoutingError(w, r, InternalError.WithCause(err))
			return
		}
		w.Header().Add("Set-Cookie", clear)
	}
	if err := writePreparedResponse(w, r, data, t.responseKind, t.responseStatus); err != nil {
		// The response is committed, so no error document can replace it. The
		// failure still reaches the request observation as a redacted diagnostic.
		recordRequestDiagnostic(r.Context(), errordiag.Describe(err))
		logRouteFailure(r, "HTTP endpoint response failed", err)
		panic(stdhttp.ErrAbortHandler)
	}
}
