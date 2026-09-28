package http

import stdhttp "net/http"

// Cookie authentication has no Authorization-header cache protection. Apply the
// safe default before all success/failure paths; explicit downstream response
// policy remains owned by the handler, as with BrowserSessions historically.
func privateCookieResponse(header stdhttp.Header) {
	header.Set("Cache-Control", "no-store")
}
