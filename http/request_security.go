package http

import (
	"context"
	"net/netip"
	"strings"
	"unicode"
)

// SecurityRequestEvent is explicitly opt-in. Path excludes the query, is bounded
// and has control characters removed. It remains untrusted and may contain
// private path values: observers must classify/redact it before retaining it.
// ClientIP uses the same trusted-proxy policy as admission, including early rejects.
type SecurityRequestEvent struct {
	Request       RequestEvent
	ClientIP      netip.Addr
	Path          string
	PathTruncated bool
}

// SecurityRequestObserver extends RequestObserver without changing default
// payload-free observations. Both callbacks run once under request ownership.
type SecurityRequestObserver interface {
	RequestObserver
	ObserveSecurityRequest(context.Context, SecurityRequestEvent)
}

const MaxSecurityPathBytes = 512

func boundedSecurityPath(path string) (string, bool) {
	truncated := len(path) > MaxSecurityPathBytes
	if truncated {
		path = path[:MaxSecurityPathBytes]
	}
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, path), truncated
}
