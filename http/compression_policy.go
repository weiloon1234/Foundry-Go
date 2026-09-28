package http

import (
	"mime"
	stdhttp "net/http"
	"strings"
)

func compressionTransformExcluded(status int, header stdhttp.Header) bool {
	if status < 200 || status == 204 || status == 205 || status == 206 || status == 304 ||
		header.Get("Content-Range") != "" || header.Get("Content-Encoding") != "" || header.Get("Upgrade") != "" ||
		len(header.Values("Set-Cookie")) != 0 || responseHasCacheDirective(header, "no-transform", "no-store", "private") {
		return true
	}
	return false
}

func compressionAllowed(status int, header stdhttp.Header) bool {
	if compressionTransformExcluded(status, header) {
		return false
	}
	contentType := header.Get("Content-Type")
	if len(contentType) > 1024 {
		return false
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	if media == "text/event-stream" || media == "application/grpc" || strings.HasPrefix(media, "application/grpc+") {
		return false
	}
	return strings.HasPrefix(media, "text/") || strings.HasSuffix(media, "+json") || strings.HasSuffix(media, "+xml") ||
		media == "application/json" || media == "application/xml" || media == "application/javascript"
}
func bodylessCompressionStatus(status int) bool {
	return status < 200 || status == 204 || status == 205 || status == 304
}
