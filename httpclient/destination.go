package httpclient

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// DestinationMode selects ordinary networking or an enforced direct-connect policy.
type DestinationMode string

const (
	UnrestrictedDestinations DestinationMode = ""
	RestrictedDestinations   DestinationMode = "restricted"
)

type Scheme string

const (
	HTTPS Scheme = "https"
	HTTP  Scheme = "http"
)

// DestinationPolicy constrains the URL and actual connected address. Empty Hosts
// allows any DNS name; empty Networks allows public Internet addresses only.
// Nonempty Networks is an exclusive allowlist, including explicitly allowed
// private networks. Restricted clients bypass environment proxies and reject
// custom transports, because either can resolve/connect outside this policy.
type DestinationPolicy struct {
	Mode     DestinationMode
	Schemes  []Scheme       `config:",json"`
	Ports    []uint16       `config:",json"`
	Hosts    []string       `config:",json"`
	Networks []netip.Prefix `config:",json"`
}

// PublicDestinations permits HTTPS on port 443 to public Internet addresses.
// Restrict Hosts for known services; explicitly add other schemes/ports if needed.
func PublicDestinations() DestinationPolicy {
	return DestinationPolicy{Mode: RestrictedDestinations, Schemes: []Scheme{HTTPS}, Ports: []uint16{443}}
}

var DestinationDenied = fault.New(fault.Invalid, "outbound destination is not permitted")

func (p DestinationPolicy) Validate() error {
	if p.Mode == UnrestrictedDestinations {
		if len(p.Schemes)+len(p.Ports)+len(p.Hosts)+len(p.Networks) != 0 {
			return invalid()
		}
		return nil
	}
	if p.Mode != RestrictedDestinations || len(p.Schemes) == 0 || len(p.Schemes) > 2 || len(p.Ports) == 0 || len(p.Ports) > 256 || len(p.Hosts) > 256 || len(p.Networks) > 256 {
		return invalid()
	}
	for _, scheme := range p.Schemes {
		if scheme != HTTP && scheme != HTTPS {
			return invalid()
		}
	}
	for _, port := range p.Ports {
		if port == 0 {
			return invalid()
		}
	}
	for _, host := range p.Hosts {
		if !destinationHost(host) {
			return invalid()
		}
	}
	for _, network := range p.Networks {
		if !network.IsValid() || network != network.Masked() || network.Addr().Is4In6() {
			return invalid()
		}
	}
	return nil
}
func (p DestinationPolicy) snapshot() DestinationPolicy {
	p.Schemes = slices.Clone(p.Schemes)
	p.Ports = slices.Clone(p.Ports)
	p.Hosts = slices.Clone(p.Hosts)
	p.Networks = slices.Clone(p.Networks)
	return p
}
func destinationHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func (p DestinationPolicy) checkURL(u *url.URL) error {
	if p.Mode == UnrestrictedDestinations {
		return nil
	}
	if u == nil || !slices.Contains(p.Schemes, Scheme(u.Scheme)) || !destinationHost(u.Hostname()) {
		return DestinationDenied
	}
	port := uint64(443)
	if u.Scheme == "http" {
		port = 80
	}
	if text := u.Port(); text != "" {
		parsed, err := strconv.ParseUint(text, 10, 16)
		if err != nil {
			return DestinationDenied
		}
		port = parsed
	}
	if !slices.Contains(p.Ports, uint16(port)) {
		return DestinationDenied
	}
	host := strings.TrimSuffix(u.Hostname(), ".")
	if len(p.Hosts) > 0 && !slices.ContainsFunc(p.Hosts, func(allowed string) bool { return strings.EqualFold(strings.TrimSuffix(allowed, "."), host) }) {
		return DestinationDenied
	}
	if ip, err := netip.ParseAddr(host); err == nil && !p.allowsAddress(ip) {
		return DestinationDenied
	}
	return nil
}

// Special-purpose ranges are excluded even when Go considers them global unicast.
// Public IPv6 is restricted to 2000::/3; translation/tunnel ranges cannot tunnel
// an otherwise disallowed IPv4 destination. Explicit Networks remain available.
var nonPublicIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	// Azure WireServer is a platform endpoint despite its globally unicast IP.
	netip.MustParsePrefix("168.63.129.16/32"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}
var nonPublicIPv6 = []netip.Prefix{
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}
var publicIPv6 = netip.MustParsePrefix("2000::/3")

func (p DestinationPolicy) allowsAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if len(p.Networks) > 0 {
		return slices.ContainsFunc(p.Networks, func(network netip.Prefix) bool { return network.Contains(address) })
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	blocked := nonPublicIPv4
	if address.Is6() {
		if !publicIPv6.Contains(address) {
			return false
		}
		blocked = nonPublicIPv6
	}
	return !slices.ContainsFunc(blocked, func(network netip.Prefix) bool { return network.Contains(address) })
}

type destinationDialer struct {
	policy  DestinationPolicy
	timeout time.Duration
	lookup  func(context.Context, string, string) ([]netip.Addr, error)
	connect func(context.Context, string, string) (net.Conn, error)
}

func newDestinationDialer(policy DestinationPolicy, timeout time.Duration) destinationDialer {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return destinationDialer{policy: policy, timeout: timeout, lookup: net.DefaultResolver.LookupNetIP, connect: dialer.DialContext}
}
func (d destinationDialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, DestinationDenied
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, DestinationDenied
	}
	operation, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	var addresses []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{ip}
	} else {
		addresses, err = d.lookup(operation, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, DestinationDenied
	}
	// Validate the complete answer before connecting to any address. Dial literals
	// so a second DNS resolution cannot replace the address that was checked.
	for _, ip := range addresses {
		if !d.policy.allowsAddress(ip) {
			return nil, DestinationDenied
		}
	}
	var last error
	for _, ip := range addresses {
		if err := operation.Err(); err != nil {
			return nil, err
		}
		connection, err := d.connect(operation, "tcp", net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return connection, nil
		}
		last = err
	}
	return nil, last
}
