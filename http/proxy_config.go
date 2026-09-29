package http

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type proxyHeaderFormat uint8

const (
	proxySingleIP proxyHeaderFormat = iota + 1
	proxyIPChain
	proxyForwardedChain
)

// ProxyHeader declares one forwarding source and its parser. Use the provided
// constructors; malformed data in the first present source never falls through
// to another header. Header priority is the order declared in TrustedProxyConfig.
type ProxyHeader struct {
	name   HeaderName
	format proxyHeaderFormat
}

// ForwardedHeader selects RFC 7239's Forwarded chain and its for parameters.
// Other parameters are parsed but do not rewrite request Host, URL or TLS state.
func ForwardedHeader() ProxyHeader { return ProxyHeader{"Forwarded", proxyForwardedChain} }

// XForwardedForHeader selects a comma-separated IP chain, walked right to left.
func XForwardedForHeader() ProxyHeader { return ProxyHeader{"X-Forwarded-For", proxyIPChain} }

// ClientIPHeader selects one IP address from a header overwritten by a trusted
// peer, such as X-Real-IP or CF-Connecting-IP. It cannot parse chain headers.
func ClientIPHeader(name HeaderName) ProxyHeader { return ProxyHeader{name, proxySingleIP} }

func (h ProxyHeader) Name() HeaderName { return h.name }

func (h ProxyHeader) Validate() error {
	name, err := h.name.Canonical()
	if err != nil {
		return err
	}
	switch h.format {
	case proxySingleIP:
		if name == "Forwarded" || name == "X-Forwarded-For" {
			return fault.New(fault.Invalid, "chain headers require their typed forwarding descriptor")
		}
	case proxyIPChain:
		if name != "X-Forwarded-For" {
			return fault.New(fault.Invalid, "invalid X-Forwarded-For descriptor")
		}
	case proxyForwardedChain:
		if name != "Forwarded" {
			return fault.New(fault.Invalid, "invalid Forwarded descriptor")
		}
	default:
		return fault.New(fault.Invalid, "proxy header requires a parser declaration")
	}
	return nil
}

// TrustedProxyConfig owns explicit peer networks and ordered header sources.
// Zero trusts no peers. Networks do not automatically include loopback, private
// networks or a CDN. A nonempty network list requires explicit IP or origin header sources.
// A trusted peer must append trustworthy chain entries or overwrite single-IP
// headers; CIDR membership alone cannot prove its forwarding configuration.
type TrustedProxyConfig struct {
	Proxies []netip.Prefix
	// Headers select client-IP forwarding sources.
	Headers []ProxyHeader
	// OriginHeaders select public scheme/authority sources independently.
	OriginHeaders []ProxyOriginHeader
}

// TrustedProxyMiddlewareID identifies the built-in client-IP policy.
const TrustedProxyMiddlewareID MiddlewareID = "foundry.trusted-proxy"

const maxProxyNetworks = 256
const maxProxyHeaders = 8
const maxProxyHops = 64

// maxProxyEntryBytes bounds one consumed hop; longer entries are unusable hops.
const maxProxyEntryBytes = 256

type proxyPolicy struct {
	proxies       []netip.Prefix
	headers       []ProxyHeader
	originHeaders []ProxyOriginHeader
}

func (c TrustedProxyConfig) Validate() error { _, err := compileTrustedProxy(c); return err }

func compileTrustedProxy(c TrustedProxyConfig) (proxyPolicy, error) {
	if len(c.Proxies) > maxProxyNetworks || len(c.Headers) > maxProxyHeaders || len(c.OriginHeaders) > maxProxyHeaders {
		return proxyPolicy{}, fault.New(fault.Invalid, "trusted proxy configuration exceeds its declaration bound")
	}
	if len(c.Proxies) != 0 && len(c.Headers) == 0 && len(c.OriginHeaders) == 0 {
		return proxyPolicy{}, fault.New(fault.Invalid, "trusted proxy networks require explicit forwarding headers")
	}
	p := proxyPolicy{proxies: make([]netip.Prefix, 0, len(c.Proxies)), headers: slices.Clone(c.Headers), originHeaders: slices.Clone(c.OriginHeaders)}
	networks := make(map[netip.Prefix]bool)
	for _, network := range c.Proxies {
		if !network.IsValid() || network.Addr().Zone() != "" {
			return proxyPolicy{}, fault.New(fault.Invalid, "trusted proxy network is invalid")
		}
		if network.Addr().Is4In6() {
			if network.Bits() < 96 {
				return proxyPolicy{}, fault.New(fault.Invalid, "mapped IPv4 proxy prefixes must include the mapped-address prefix")
			}
			network = netip.PrefixFrom(network.Addr().Unmap(), network.Bits()-96)
		}
		network = network.Masked()
		if networks[network] {
			return proxyPolicy{}, fault.New(fault.Duplicate, "trusted proxy network is repeated")
		}
		networks[network] = true
		p.proxies = append(p.proxies, network)
	}
	headers := make(map[string]bool)
	for i, header := range p.headers {
		if err := header.Validate(); err != nil {
			return proxyPolicy{}, err
		}
		name, _ := header.name.Canonical()
		key := strings.ToLower(string(name))
		if headers[key] {
			return proxyPolicy{}, fault.New(fault.Duplicate, "trusted proxy header is repeated")
		}
		headers[key] = true
		p.headers[i].name = name
	}
	if err := validateProxyOriginHeaders(p.originHeaders); err != nil {
		return proxyPolicy{}, err
	}
	return p, nil
}

func (p proxyPolicy) trusts(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	for _, network := range p.proxies {
		if network.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}
