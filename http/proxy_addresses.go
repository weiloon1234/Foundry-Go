package http

import (
	"net/netip"
	"strconv"
	"strings"
)

func parseProxyAddresses(source ProxyHeader, values []string) ([]netip.Addr, error) {
	if err := validateProxyHeaderValues(values); err != nil {
		return nil, err
	}
	if source.format == proxyForwardedChain {
		return parseForwardedIPs(strings.Join(values, ","))
	}
	var addresses []netip.Addr
	for _, value := range values {
		for raw := range strings.SplitSeq(value, ",") {
			if len(addresses) == maxProxyHops {
				return nil, BadRequest
			}
			ip, err := netip.ParseAddr(strings.Trim(raw, " \t"))
			if err != nil || ip.Zone() != "" {
				return nil, BadRequest
			}
			addresses = append(addresses, ip.Unmap())
		}
	}
	if source.format == proxySingleIP && len(addresses) != 1 {
		return nil, BadRequest
	}
	return addresses, nil
}

// An invalid address with no error is a valid unknown/obfuscated node. It is a
// trust boundary and must never be skipped to accept an earlier client value.
func forwardedNode(raw string) (netip.Addr, error) {
	var node, port string
	var hasPort bool
	bracketed := strings.HasPrefix(raw, "[")
	if bracketed {
		end := strings.IndexByte(raw, ']')
		if end < 0 {
			return netip.Addr{}, BadRequest
		}
		node = raw[1:end]
		if end+1 < len(raw) {
			if raw[end+1] != ':' {
				return netip.Addr{}, BadRequest
			}
			port = raw[end+2:]
			hasPort = true
		}
	} else {
		node, port, hasPort = strings.Cut(raw, ":")
	}
	if hasPort {
		if strings.HasPrefix(port, "_") {
			if !obfuscatedNode(port) {
				return netip.Addr{}, BadRequest
			}
		} else {
			if _, err := strconv.ParseUint(port, 10, 16); err != nil {
				return netip.Addr{}, BadRequest
			}
		}
	}
	if !bracketed && (strings.EqualFold(node, "unknown") || obfuscatedNode(node)) {
		return netip.Addr{}, nil
	}
	ip, err := netip.ParseAddr(node)
	if err != nil || ip.Zone() != "" || bracketed != ip.Is6() {
		return netip.Addr{}, BadRequest
	}
	return ip.Unmap(), nil
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

func validateProxyHeaderValues(values []string) error {
	if len(values) == 0 || len(values) > maxProxyHops {
		return BadRequest
	}
	size := len(values) - 1
	for _, value := range values {
		if len(value) > maxProxyHeaderBytes-size {
			return BadRequest
		}
		size += len(value)
	}
	return nil
}
