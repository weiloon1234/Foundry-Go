package http

import (
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type proxyOriginFormat uint8

const (
	proxyOriginForwarded proxyOriginFormat = iota + 1
	proxyOriginPair
	proxyOriginScheme
)

// ProxyOriginHeader declares an origin source from an explicitly trusted peer.
// Use a constructor. First-present priority never falls through malformed data.
type ProxyOriginHeader struct {
	scheme HeaderName
	host   HeaderName
	format proxyOriginFormat
}

// ForwardedOriginHeader selects host and proto from the same RFC 7239 element
// at the trusted chain boundary. Both parameters are required on that element.
// Unknown/missing for values stop traversal; earlier elements are never skipped.
func ForwardedOriginHeader() ProxyOriginHeader {
	return ProxyOriginHeader{"Forwarded", "", proxyOriginForwarded}
}

// XForwardedOriginHeaders uses one X-Forwarded-Proto and X-Forwarded-Host value.
// The trusted edge must overwrite both fields. Comma lists are rejected because
// independently appended X-Forwarded lists do not prove a shared hop boundary.
func XForwardedOriginHeaders() ProxyOriginHeader {
	return ProxyOriginHeaders("X-Forwarded-Proto", "X-Forwarded-Host")
}

// ProxyOriginHeaders selects an overwritten single scheme/authority header pair.
// Use ForwardedOriginHeader when a multi-hop origin chain is required.
func ProxyOriginHeaders(scheme, authority HeaderName) ProxyOriginHeader {
	return ProxyOriginHeader{scheme, authority, proxyOriginPair}
}

// ProxySchemeHeader selects one overwritten scheme header while preserving the
// native request Host. This is suitable when the proxy preserves the public Host.
func ProxySchemeHeader(scheme HeaderName) ProxyOriginHeader {
	return ProxyOriginHeader{scheme, "", proxyOriginScheme}
}

func (h ProxyOriginHeader) Validate() error {
	scheme, err := h.scheme.Canonical()
	if err != nil {
		return err
	}
	switch h.format {
	case proxyOriginForwarded:
		if scheme == "Forwarded" && h.host == "" {
			return nil
		}
	case proxyOriginScheme:
		if scheme != "Forwarded" && scheme != "Host" && h.host == "" {
			return nil
		}
	case proxyOriginPair:
		host, err := h.host.Canonical()
		if err != nil {
			return err
		}
		if scheme != host && scheme != "Forwarded" && host != "Forwarded" && scheme != "Host" && host != "Host" {
			return nil
		}
	}
	return fault.New(fault.Invalid, "invalid proxy origin header descriptor")
}

func validateProxyOriginHeaders(headers []ProxyOriginHeader) error {
	seen := make(map[HeaderName]bool)
	for i, h := range headers {
		if err := h.Validate(); err != nil {
			return err
		}
		h.scheme, _ = h.scheme.Canonical()
		if h.host != "" {
			h.host, _ = h.host.Canonical()
		}
		for _, name := range []HeaderName{h.scheme, h.host} {
			if name == "" {
				continue
			}
			if seen[name] {
				return fault.New(fault.Duplicate, "proxy origin header source is repeated")
			}
			seen[name] = true
		}
		headers[i] = h
	}
	return nil
}

type proxyRequestOrigin struct{ scheme, host string }
type proxyOriginContextKey struct{}

func (p proxyPolicy) forwardedOrigin(r *stdhttp.Request) (proxyRequestOrigin, error) {
	peer := PeerIP(r)
	if !p.trusts(peer) {
		return proxyRequestOrigin{}, nil
	}
	for _, source := range p.originHeaders {
		values := r.Header.Values(string(source.scheme))
		hosts := r.Header.Values(string(source.host))
		if len(values) == 0 && len(hosts) == 0 {
			continue
		}
		var origin proxyRequestOrigin
		if source.format == proxyOriginForwarded {
			if err := validateProxyHeaderValues(values); err != nil {
				return proxyRequestOrigin{}, err
			}
			chain, err := parseForwarded(strings.Join(values, ","))
			if err != nil {
				return proxyRequestOrigin{}, err
			}
			selected := -1
			for i := len(chain) - 1; i >= 0 && p.trusts(peer); i-- {
				selected = i
				if !chain[i].address.IsValid() {
					break
				}
				peer = chain[i].address
			}
			if selected < 0 {
				return proxyRequestOrigin{}, BadRequest
			}
			origin = proxyRequestOrigin{chain[selected].scheme, chain[selected].host}
		} else {
			if len(values) != 1 || len(values[0]) > maxOriginBytes || strings.Contains(values[0], ",") {
				return proxyRequestOrigin{}, BadRequest
			}
			origin.scheme = strings.Trim(values[0], " \t")
			if source.format == proxyOriginPair {
				if len(hosts) != 1 || len(hosts[0]) > maxOriginBytes || strings.Contains(hosts[0], ",") {
					return proxyRequestOrigin{}, BadRequest
				}
				origin.host = strings.Trim(hosts[0], " \t")
			} else {
				origin.host = r.Host
			}
		}
		origin.scheme = strings.ToLower(origin.scheme)
		if origin.scheme != "http" && origin.scheme != "https" {
			return proxyRequestOrigin{}, BadRequest
		}
		// Shared origin grammar prevents userinfo, path/query fragments, invalid
		// authorities and control characters from becoming a public URL base.
		if _, err := ParseOrigin(origin.scheme + "://" + origin.host); err != nil {
			return proxyRequestOrigin{}, BadRequest
		}
		return origin, nil
	}
	return proxyRequestOrigin{}, nil
}

// IsSecure reports the validated public request scheme when TrustedProxy has
// supplied one, otherwise native TLS. A configured URL base and arbitrary headers
// never make a request secure. Run TrustedProxy before security/cookie policy.
func IsSecure(r *stdhttp.Request) bool {
	if r == nil {
		return false
	}
	if origin, ok := r.Context().Value(proxyOriginContextKey{}).(proxyRequestOrigin); ok && origin.scheme != "" {
		return origin.scheme == "https"
	}
	return r.TLS != nil
}

func requestOrigin(r *stdhttp.Request) (Origin, error) {
	if r == nil {
		return "", BadRequest
	}
	if origin, ok := r.Context().Value(proxyOriginContextKey{}).(proxyRequestOrigin); ok && origin.scheme != "" {
		return ParseOrigin(origin.scheme + "://" + origin.host)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return ParseOrigin(scheme + "://" + r.Host)
}
