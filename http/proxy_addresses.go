package http

import (
	"net/netip"
	"strconv"
	"strings"
)

// proxyHopAddress parses one X-Forwarded-For or single-IP entry. Bare IPv4/IPv6,
// ip:port and [IPv6]:port spellings are accepted because common load balancers
// append ports. Unknown, obfuscated and malformed entries return an invalid
// address: callers treat that as a trust boundary, never as a skippable hop.
func proxyHopAddress(raw string) netip.Addr {
	raw = strings.Trim(raw, " \t")
	if raw == "" || len(raw) > maxProxyEntryBytes {
		return netip.Addr{}
	}
	if ip, err := netip.ParseAddr(raw); err == nil {
		if ip.Zone() != "" {
			return netip.Addr{}
		}
		return ip.Unmap()
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		ip, err := netip.ParseAddr(raw[1 : len(raw)-1])
		if err != nil || ip.Zone() != "" || !ip.Is6() {
			return netip.Addr{}
		}
		return ip.Unmap()
	}
	peer, err := netip.ParseAddrPort(raw)
	if err != nil || peer.Addr().Zone() != "" {
		return netip.Addr{}
	}
	return peer.Addr().Unmap()
}

// reverseProxyEntries visits non-empty comma-separated list members from the
// nearest hop outward, stopping when visit returns false or after maxProxyHops.
// Entries beyond the walk's trust boundary are never inspected, so client-owned
// prefixes cannot reject a request or influence the resolved address.
func reverseProxyEntries(values []string, visit func(string) bool) {
	hops := 0
	for line := len(values) - 1; line >= 0; line-- {
		rest := values[line]
		for {
			comma := strings.LastIndexByte(rest, ',')
			entry := strings.Trim(rest[comma+1:], " \t")
			if entry != "" {
				if hops == maxProxyHops || !visit(entry) {
					return
				}
				hops++
			}
			if comma < 0 {
				break
			}
			rest = rest[:comma]
		}
	}
}

// forwardedHops returns at most maxProxyHops RFC 7239 elements, nearest first.
// Each header line is tokenized independently, so an unterminated client quote
// cannot swallow a later line appended by a trusted proxy. Malformed elements
// are retained as unusable boundaries instead of failing the whole request.
func forwardedHops(values []string) []forwardedElement {
	var hops []forwardedElement
	for line := len(values) - 1; line >= 0 && len(hops) < maxProxyHops; line-- {
		elements := forwardedLineElements(values[line], maxProxyHops-len(hops))
		for i := len(elements) - 1; i >= 0; i-- {
			hops = append(hops, parseForwardedElement(elements[i]))
		}
	}
	return hops
}

// An invalid address with ok=true is a valid unknown/obfuscated node. It is a
// trust boundary and must never be skipped to accept an earlier client value.
// Lenient spellings (bare IPv6, ip:port) are accepted; anything else is not ok.
func forwardedNode(raw string) (netip.Addr, bool) {
	if raw == "" || len(raw) > maxProxyEntryBytes {
		return netip.Addr{}, false
	}
	var node, port string
	var hasPort bool
	bracketed := strings.HasPrefix(raw, "[")
	switch {
	case bracketed:
		end := strings.IndexByte(raw, ']')
		if end < 0 {
			return netip.Addr{}, false
		}
		node = raw[1:end]
		if end+1 < len(raw) {
			if raw[end+1] != ':' {
				return netip.Addr{}, false
			}
			port = raw[end+2:]
			hasPort = true
		}
	case strings.Count(raw, ":") > 1:
		// A non-compliant bare IPv6 literal (RFC 7239 requires brackets).
		node = raw
	default:
		node, port, hasPort = strings.Cut(raw, ":")
	}
	if hasPort {
		if strings.HasPrefix(port, "_") {
			if !obfuscatedNode(port) {
				return netip.Addr{}, false
			}
		} else if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return netip.Addr{}, false
		}
	}
	if !bracketed && (strings.EqualFold(node, "unknown") || obfuscatedNode(node)) {
		return netip.Addr{}, true
	}
	ip, err := netip.ParseAddr(node)
	if err != nil || ip.Zone() != "" || bracketed && !ip.Is6() {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

func obfuscatedNode(value string) bool {
	if len(value) < 2 || value[0] != '_' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
