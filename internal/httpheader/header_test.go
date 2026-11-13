package httpheader_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpheader"
)

func TestRequestNameOwnership(t *testing.T) {
	reserved := []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Cookie2", "Set-Cookie",
		"Content-Type", "Content-Custom", "Host", "Connection", "Keep-Alive", "Proxy-Connection",
		"Transfer-Encoding", "TE", "Trailer", "Upgrade", "Expect", "HTTP2-Settings",
		"Origin", "Sec-Custom", "Access-Control-Custom", "X-CSRF-Token", "X-XSRF-Token",
		"Accept", "Accept-Encoding", "Accept-Language", "Range", "If-Range", "If-Match",
		"If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "Idempotency-Key",
		"Last-Event-ID", "X-Request-ID", "Forwarded", "X-Forwarded-Custom", "X-Real-IP",
		"traceparent", "tracestate", "X-Accel-Redirect", "X-Sendfile", "X-Lighttpd-Custom",
		"X-Content-Type-Options", "X-Frame-Options", "X-Xss-Protection",
		"X-Permitted-Cross-Domain-Policies", "X-Dns-Prefetch-Control",
	}
	for _, name := range reserved {
		for _, spelling := range []string{name, strings.ToLower(name), strings.ToUpper(name)} {
			if !httpheader.ReservedRequestName(spelling) || !errors.Is(httpheader.ValidateRequestName(spelling), fault.Invalid) {
				t.Errorf("accepted reserved header %q", spelling)
			}
		}
	}
	for _, name := range []string{"X-App-Version", "Device-ID", "If-App-Version", "Prefer", "X-Tenant", "Contentish", "Section", "X-Forwardedish", "X-Request-ID-Suffix", "X-Sendfile-Suffix", "CF-Connecting-IP"} {
		if httpheader.ReservedRequestName(name) || httpheader.ValidateRequestName(name) != nil {
			t.Errorf("rejected application header %q", name)
		}
	}
}

func TestNamePrimitives(t *testing.T) {
	for _, name := range []string{"", "x y", "x:y", "é", "x\r\ny", strings.Repeat("x", httpheader.MaxNameBytes+1)} {
		if canonical, err := httpheader.CanonicalName(name); canonical != "" || !errors.Is(err, fault.Invalid) {
			t.Errorf("invalid name %q: %q, %v", name, canonical, err)
		}
	}
	for input, want := range map[string]string{
		"x-device-ID": "X-Device-Id", "x_custom~name": "X_custom~name",
		"!#$%&'*+.^_`|~": "!#$%&'*+.^_`|~", strings.Repeat("x", httpheader.MaxNameBytes): "X" + strings.Repeat("x", httpheader.MaxNameBytes-1),
	} {
		if got, err := httpheader.CanonicalName(input); err != nil || got != want {
			t.Errorf("CanonicalName(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestValuePrimitivesPreserveResponseAndRestrictRequest(t *testing.T) {
	for _, value := range []string{"", " a\tb ", "a,b+%20", strings.Repeat("a", httpheader.MaxValueBytes)} {
		if err := httpheader.ValidateRequestValue(value); err != nil {
			t.Errorf("rejected portable value: %v", err)
		}
	}
	for _, value := range []string{"\x80", "é"} {
		if err := httpheader.ValidateValue(value); err != nil {
			t.Errorf("changed native response semantics: %v", err)
		}
		if err := httpheader.ValidateRequestValue(value); !errors.Is(err, fault.Invalid) {
			t.Errorf("accepted non-ASCII request value: %v", err)
		}
	}
	for _, value := range []string{"\r", "\n", "\x00", "\x1f", "\x7f", strings.Repeat("a", httpheader.MaxValueBytes+1)} {
		if httpheader.ValidateValue(value) == nil || httpheader.ValidateRequestValue(value) == nil {
			t.Error("accepted invalid header value")
		}
	}
}

func TestResponseOwnershipRemainsDirectional(t *testing.T) {
	for _, name := range []string{"Content-Language", "Location", "Vary", "X-App-Version", "X-Forwardedish"} {
		if httpheader.ReservedApplicationName(name) {
			t.Errorf("expanded response reservation to %q", name)
		}
	}
	for _, name := range []string{"X-Forwarded-", "X-Forwarded-For", "X-Request-ID", "X-CSRF-Token", "X-Accel-Redirect"} {
		if !httpheader.ReservedApplicationName(strings.ToUpper(name)) {
			t.Errorf("lost response reservation %q", name)
		}
	}
}
