package http

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Origin is an HTTP(S) origin without a path, credentials, query or fragment.
// Declarations may use ASCII hostnames, including punycode, or IP literals.
// ParseOrigin canonicalizes hostname casing, IP literals and default ports.
type Origin string

// NullOrigin explicitly permits the serialized opaque-origin value "null".
const NullOrigin Origin = "null"

const maxOriginBytes = 2048

func (o Origin) Validate() error { _, err := ParseOrigin(string(o)); return err }

// ParseOrigin validates a declaration and returns its normalized authority.
// It does not resolve DNS or convert Unicode hostnames to ASCII.
func ParseOrigin(input string) (Origin, error) {
	invalid := func() (Origin, error) {
		return "", fault.New(fault.Invalid, "HTTP origin requires an HTTP(S) authority without path, credentials, query or fragment")
	}
	if input == string(NullOrigin) {
		return NullOrigin, nil
	}
	if input == "" || len(input) > maxOriginBytes || strings.ContainsAny(input, "\\#%") {
		return invalid()
	}
	u, err := url.Parse(input)
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return invalid()
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return invalid()
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return invalid()
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
		if ip.Is6() {
			host = "[" + host + "]"
		}
	} else {
		for i := range host {
			c := host[i]
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
				continue
			}
			return invalid()
		}
		if strings.ContainsAny(u.Host, "[]") {
			return invalid()
		}
	}
	port := u.Port()
	if port == "" && strings.HasSuffix(u.Host, ":") {
		return invalid()
	}
	if port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return invalid()
		}
		if !(scheme == "http" && number == 80 || scheme == "https" && number == 443) {
			host += ":" + strconv.FormatUint(number, 10)
		}
	}
	return Origin(scheme + "://" + host), nil
}
