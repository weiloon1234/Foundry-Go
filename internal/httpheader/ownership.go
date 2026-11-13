package httpheader

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const (
	IdempotencyKey = "Idempotency-Key"
	RequestID      = "X-Request-ID"
	TraceParent    = "traceparent"
	TraceState     = "tracestate"
)

// ReservedApplicationName identifies the existing proxy/security X-* ownership
// shared by request declarations and application response headers.
func ReservedApplicationName(name string) bool {
	name = strings.ToLower(name)
	switch name {
	case "x-sendfile", "x-real-ip", "x-content-type-options", "x-frame-options",
		"x-xss-protection", strings.ToLower(RequestID), "x-permitted-cross-domain-policies",
		"x-dns-prefetch-control":
		return true
	}
	return strings.HasPrefix(name, "x-accel-") || strings.HasPrefix(name, "x-lighttpd-") ||
		strings.HasPrefix(name, "x-forwarded-") || strings.HasPrefix(name, "x-csrf-") ||
		strings.HasPrefix(name, "x-xsrf-")
}

// ReservedRequestName matches static framework/transport ownership regardless
// of casing. Configured proxy headers and third-party middleware inputs must
// also not be declared, but are not inferred by this static policy.
func ReservedRequestName(name string) bool {
	name = strings.ToLower(name)
	switch name {
	case "authorization", "proxy-authorization", "cookie", "cookie2", "set-cookie",
		"host", "connection", "keep-alive", "proxy-connection", "transfer-encoding",
		"te", "trailer", "upgrade", "expect", "http2-settings", "origin",
		"accept", "accept-encoding", "accept-language", "range", "if-range",
		"if-match", "if-none-match", "if-modified-since", "if-unmodified-since",
		strings.ToLower(IdempotencyKey), "last-event-id", "forwarded", TraceParent, TraceState:
		return true
	}
	return strings.HasPrefix(name, "content-") || strings.HasPrefix(name, "sec-") ||
		strings.HasPrefix(name, "access-control-") || ReservedApplicationName(name)
}

// ValidateRequestName rejects invalid names and static reserved request inputs.
func ValidateRequestName(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if ReservedRequestName(name) {
		return safeerror.NewFault(fault.Invalid, "request header is owned by the framework or transport")
	}
	return nil
}
