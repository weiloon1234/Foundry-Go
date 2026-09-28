package http

import (
	"mime"
	stdhttp "net/http"
	"strings"
)

// Automatic validators cover complete successful representations. Existing
// validators and partial/streaming protocols retain their handler's semantics.
func automaticETagAllowed(status int, header stdhttp.Header) bool {
	if status < 200 || status >= 300 || status == stdhttp.StatusNoContent ||
		status == stdhttp.StatusResetContent || status == stdhttp.StatusPartialContent ||
		len(header.Values("ETag")) != 0 ||
		header.Get("Content-Range") != "" || header.Get("Upgrade") != "" ||
		hasResponseTrailers(header) {
		return false
	}
	if responseHasCacheDirective(header, "no-store") {
		return false
	}
	media, _, _ := mime.ParseMediaType(header.Get("Content-Type"))
	return media != "text/event-stream" && media != "application/grpc" &&
		!strings.HasPrefix(media, "application/grpc+")
}
