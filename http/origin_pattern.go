package http

import (
	"net/netip"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// OriginPattern declares an additional browser origin that is not an exact
// HTTP(S) Origin: either an exact non-web origin serialized by an embedded
// webview or extension, such as capacitor://localhost, tauri://localhost or
// chrome-extension://<id>, or an HTTP(S) subdomain wildcard such as
// https://*.example.com (optionally with a port). A wildcard matches one or
// more complete labels before its suffix, never the bare suffix. Bare "*",
// partial-label wildcards, single-label suffixes and IP literals are rejected.
// Public-suffix knowledge is not built in; do not declare *.co.uk-style suffixes.
type OriginPattern string

func (p OriginPattern) Validate() error { _, err := compileOriginPattern(p); return err }

// Wildcard reports whether the declaration matches a family of subdomains.
func (p OriginPattern) Wildcard() bool { return strings.Contains(string(p), "://*.") }

type originMatcher struct {
	// exact is the canonical serialized form of a non-web origin.
	exact string
	// scheme, suffix and port describe an HTTP(S) subdomain wildcard. The
	// suffix includes its leading dot; port is empty for the scheme default.
	scheme, suffix, port string
}

func compileOriginPattern(p OriginPattern) (originMatcher, error) {
	invalid := func() (originMatcher, error) {
		return originMatcher{}, fault.New(fault.Invalid, "origin pattern requires an exact non-HTTP(S) origin or an HTTP(S) subdomain wildcard such as https://*.example.com")
	}
	text := string(p)
	if len(text) > maxOriginBytes {
		return invalid()
	}
	scheme, rest, ok := strings.Cut(text, "://")
	if !ok {
		return invalid()
	}
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		exact, ok := canonicalAppOrigin(text)
		if !ok {
			return invalid()
		}
		return originMatcher{exact: exact}, nil
	}
	if !strings.HasPrefix(rest, "*.") {
		return invalid()
	}
	// Reuse the shared HTTP(S) origin grammar on a concrete representative.
	origin, err := ParseOrigin(scheme + "://wildcard." + rest[2:])
	if err != nil {
		return invalid()
	}
	host, port := splitOriginAuthority(string(origin))
	suffix := strings.TrimPrefix(host, "wildcard.")
	if strings.Contains(suffix, "*") || !strings.Contains(suffix, ".") || !completeLabels(suffix) {
		return invalid()
	}
	if _, err := netip.ParseAddr(suffix); err == nil {
		return invalid()
	}
	return originMatcher{scheme: scheme, suffix: "." + suffix, port: port}, nil
}

// matches compares a request Origin value that already passed the shared
// grammar for its kind. Canonical HTTP(S) and non-web forms never overlap.
func (m originMatcher) matches(canonical string) bool {
	if m.exact != "" {
		return canonical == m.exact
	}
	scheme, _, ok := strings.Cut(canonical, "://")
	if !ok || scheme != m.scheme {
		return false
	}
	host, port := splitOriginAuthority(canonical)
	if port != m.port || len(host) <= len(m.suffix) || !strings.HasSuffix(host, m.suffix) {
		return false
	}
	return completeLabels(host[:len(host)-len(m.suffix)])
}

// splitOriginAuthority splits a canonical ParseOrigin result. IPv6 literals are
// returned bracketed; they never match a hostname suffix.
func splitOriginAuthority(origin string) (host, port string) {
	_, authority, _ := strings.Cut(origin, "://")
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		host, authority = authority[:end+1], authority[end+1:]
		return host, strings.TrimPrefix(authority, ":")
	}
	host, port, _ = strings.Cut(authority, ":")
	return host, port
}

func completeLabels(host string) bool {
	if host == "" {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

// canonicalAppOrigin validates the serialized origin of a non-HTTP(S) scheme:
// scheme "://" host [":" port], with no credentials, path, query or fragment.
// Hosts are ASCII labels (extension IDs, localhost); case is normalized.
func canonicalAppOrigin(input string) (string, bool) {
	if input == "" || len(input) > maxOriginBytes {
		return "", false
	}
	scheme, authority, ok := strings.Cut(input, "://")
	if !ok || !validURIScheme(scheme) {
		return "", false
	}
	scheme = strings.ToLower(scheme)
	if scheme == "http" || scheme == "https" {
		return "", false
	}
	host, port, hasPort := strings.Cut(authority, ":")
	if host == "" || hasPort && (port == "" || len(port) > 5) {
		return "", false
	}
	for i := range len(port) {
		if port[i] < '0' || port[i] > '9' {
			return "", false
		}
	}
	for i := range len(host) {
		c := host[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			continue
		}
		return "", false
	}
	canonical := scheme + "://" + strings.ToLower(host)
	if hasPort {
		canonical += ":" + port
	}
	return canonical, true
}

// validURIScheme follows RFC 3986: ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ).
func validURIScheme(scheme string) bool {
	if scheme == "" || len(scheme) > 64 {
		return false
	}
	for i := range len(scheme) {
		c := scheme[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.') {
			continue
		}
		return false
	}
	return true
}
