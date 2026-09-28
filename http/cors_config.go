package http

import (
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// CORSConfig declares browser response sharing. The zero value permits no
// origins or preflight methods. Lists and wildcard flags are mutually exclusive.
// AnyHeaders/AnyMethod answer with validated requested names/methods, including
// credentialed preflights. AnyOrigin cannot be combined with Credentials.
type CORSConfig struct {
	Origins       []Origin
	AnyOrigin     bool
	Methods       []Method
	AnyMethod     bool
	Headers       []HeaderName
	AnyHeaders    bool
	ExposeHeaders []HeaderName
	Credentials   bool
	// MaxAge is a whole number of seconds from zero through 24 hours.
	// Zero explicitly disables preflight caching.
	MaxAge time.Duration
}

// CORSMiddlewareID is the built-in policy's identity for middleware inspection.
const CORSMiddlewareID MiddlewareID = "foundry.cors"
const maxCORSItems = 256
const maxCORSHeaderBytes = 8192

type corsPolicy struct {
	origins                                       map[Origin]bool
	methods                                       map[Method]bool
	headers                                       map[string]bool
	anyOrigin, anyMethod, anyHeaders, credentials bool
	expose                                        string
	maxAge                                        time.Duration
}

func (c CORSConfig) Validate() error { _, err := compileCORS(c); return err }

func compileCORS(c CORSConfig) (corsPolicy, error) {
	if len(c.Origins) > maxCORSItems || len(c.Methods) > maxCORSItems || len(c.Headers) > maxCORSItems || len(c.ExposeHeaders) > maxCORSItems {
		return corsPolicy{}, fault.New(fault.Invalid, "CORS declaration exceeds its item bound")
	}
	if c.AnyOrigin && (len(c.Origins) != 0 || c.Credentials) || c.AnyMethod && len(c.Methods) != 0 || c.AnyHeaders && len(c.Headers) != 0 {
		return corsPolicy{}, fault.New(fault.Invalid, "CORS wildcard configuration conflicts with an explicit list or credentials")
	}
	if c.MaxAge < 0 || c.MaxAge > 24*time.Hour || c.MaxAge%time.Second != 0 {
		return corsPolicy{}, fault.New(fault.Invalid, "CORS max age must be whole seconds from zero through 24 hours")
	}
	policy := corsPolicy{origins: make(map[Origin]bool), methods: make(map[Method]bool), anyOrigin: c.AnyOrigin, anyMethod: c.AnyMethod, anyHeaders: c.AnyHeaders, credentials: c.Credentials, maxAge: c.MaxAge}
	for _, item := range c.Origins {
		origin, err := ParseOrigin(string(item))
		if err != nil {
			return corsPolicy{}, err
		}
		if policy.origins[origin] {
			return corsPolicy{}, fault.New(fault.Duplicate, "CORS origin is repeated")
		}
		policy.origins[origin] = true
	}
	for _, method := range c.Methods {
		if !method.valid() {
			return corsPolicy{}, fault.New(fault.Invalid, "CORS method is not a supported framework HTTP method")
		}
		if policy.methods[method] {
			return corsPolicy{}, fault.New(fault.Duplicate, "CORS method is repeated")
		}
		policy.methods[method] = true
	}
	var err error
	policy.headers, _, err = corsHeaderDeclarations(c.Headers)
	if err != nil {
		return corsPolicy{}, err
	}
	_, exposed, err := corsHeaderDeclarations(c.ExposeHeaders)
	if err != nil {
		return corsPolicy{}, err
	}
	policy.expose = strings.Join(exposed, ", ")
	return policy, nil
}

func corsHeaderDeclarations(names []HeaderName) (map[string]bool, []string, error) {
	seen := make(map[string]bool, len(names))
	canonical := make([]string, 0, len(names))
	size := 0
	for _, name := range names {
		value, err := name.Canonical()
		if err != nil || name == "*" {
			return nil, nil, fault.New(fault.Invalid, "CORS headers require explicit valid field names; use AnyHeaders for request-header reflection")
		}
		key := strings.ToLower(string(value))
		if seen[key] {
			return nil, nil, fault.New(fault.Duplicate, "CORS header is repeated")
		}
		size += len(value) + 2
		if size > maxCORSHeaderBytes {
			return nil, nil, fault.New(fault.Invalid, "CORS header declaration exceeds its byte bound")
		}
		seen[key] = true
		canonical = append(canonical, string(value))
	}
	return seen, canonical, nil
}
