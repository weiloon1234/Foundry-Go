package http

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SecurityHeadersConfig declares response defaults before the next handler.
// Start with DefaultSecurityHeadersConfig. Zero emits no headers. HSTS is opt-in
// and applies to native TLS or a scheme validated by preceding TrustedProxy
// middleware. Arbitrary forwarding headers are ignored. Extra is an explicit native-header
// extension and does not validate arbitrary header-specific languages.
type SecurityHeadersConfig struct {
	NoSniff                bool
	Frame                  FramePolicy
	Referrer               ReferrerPolicy
	HSTS                   value.Optional[HSTSPolicy]
	DisableLegacyXSSFilter bool
	Extra                  []ResponseHeader
}

// DefaultSecurityHeadersConfig returns independent response defaults. HSTS is
// omitted until explicitly configured; no subdomains or preload are implicit.
func DefaultSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{NoSniff: true, Frame: FrameDeny, Referrer: ReferrerStrictOriginWhenCrossOrigin, DisableLegacyXSSFilter: true}
}

func (c SecurityHeadersConfig) Validate() error { _, err := compileSecurityHeaders(c); return err }

const maxSecurityExtraHeaders = 64
const maxSecurityHeaderBytes = 32 << 10

type securityHeaderPolicy struct {
	headers   []ResponseHeader
	hsts      HeaderValue
	writeHSTS bool
}

func compileSecurityHeaders(c SecurityHeadersConfig) (securityHeaderPolicy, error) {
	if err := c.Frame.Validate(); err != nil {
		return securityHeaderPolicy{}, err
	}
	if err := c.Referrer.Validate(); err != nil {
		return securityHeaderPolicy{}, err
	}
	if len(c.Extra) > maxSecurityExtraHeaders {
		return securityHeaderPolicy{}, fault.New(fault.Invalid, "security headers exceed their declaration bound")
	}
	p := securityHeaderPolicy{}
	if c.NoSniff {
		p.headers = append(p.headers, ResponseHeader{"X-Content-Type-Options", "nosniff"})
	}
	if c.Frame != "" {
		p.headers = append(p.headers, ResponseHeader{"X-Frame-Options", HeaderValue(c.Frame)})
	}
	if c.Referrer != "" {
		p.headers = append(p.headers, ResponseHeader{"Referrer-Policy", HeaderValue(c.Referrer)})
	}
	if c.DisableLegacyXSSFilter {
		p.headers = append(p.headers, ResponseHeader{"X-Xss-Protection", "0"})
	}
	if hsts, present := c.HSTS.Get(); present {
		if err := hsts.Validate(); err != nil {
			return securityHeaderPolicy{}, err
		}
		p.hsts = hsts.value()
		p.writeHSTS = true
	}
	seen := make(map[string]bool)
	size := len(p.hsts)
	if p.writeHSTS {
		size += len("Strict-Transport-Security") + 4
	}
	for _, header := range p.headers {
		size += len(header.Name) + len(header.Value) + 4
	}
	for _, header := range c.Extra {
		if err := header.Validate(); err != nil {
			return securityHeaderPolicy{}, err
		}
		name, _ := header.Name.Canonical()
		key := strings.ToLower(string(name))
		if reservedSecurityHeader(key) {
			return securityHeaderPolicy{}, fault.New(fault.Invalid, "custom security headers cannot replace typed policy or HTTP framing headers")
		}
		if seen[key] {
			return securityHeaderPolicy{}, fault.New(fault.Duplicate, "custom security header is repeated")
		}
		seen[key] = true
		size += len(name) + len(header.Value) + 4
		if size > maxSecurityHeaderBytes {
			return securityHeaderPolicy{}, fault.New(fault.Invalid, "security headers exceed their byte bound")
		}
		p.headers = append(p.headers, ResponseHeader{name, header.Value})
	}
	return p, nil
}

func reservedSecurityHeader(name string) bool {
	switch name {
	case "x-content-type-options", "x-frame-options", "referrer-policy", "strict-transport-security", "x-xss-protection",
		"content-length", "transfer-encoding", "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "upgrade":
		return true
	}
	return false
}
