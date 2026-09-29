package http

import (
	"path"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// CORSConfig declares browser response sharing. The zero value permits no
// origins or preflight methods. Lists and wildcard flags are mutually exclusive.
// AnyHeaders/AnyMethod answer with validated requested names/methods, including
// credentialed preflights. AnyOrigin cannot be combined with Credentials.
type CORSConfig struct {
	Origins []Origin
	// OriginPatterns adds exact non-HTTP(S) origins (capacitor://localhost,
	// tauri://localhost, chrome-extension://<id>) and validated HTTP(S)
	// subdomain wildcards (https://*.example.com). A matching request Origin is
	// reflected exactly. Unlisted and malformed origins receive no sharing
	// headers; ordinary requests still reach the handler.
	OriginPatterns []OriginPattern
	AnyOrigin      bool
	Methods        []Method
	AnyMethod      bool
	Headers        []HeaderName
	AnyHeaders     bool
	ExposeHeaders  []HeaderName
	Credentials    bool
	// MaxAge is a whole number of seconds from zero through 24 hours.
	// Zero explicitly disables preflight caching.
	MaxAge time.Duration
	// Paths selects a complete replacement policy for request paths equal to
	// or below a prefix; the longest matching prefix wins and other requests
	// use this policy. Path policies cannot declare nested Paths.
	Paths []CORSPath
}

// CORSPath applies Policy to requests whose decoded URL path equals Prefix or
// continues below it at a segment boundary (/api matches /api and /api/x, not
// /apix). Prefix is an absolute, clean path without a trailing slash.
type CORSPath struct {
	Prefix string
	Policy CORSConfig
}

// CORSMiddlewareID is the built-in policy's identity for middleware inspection.
const CORSMiddlewareID MiddlewareID = "foundry.cors"
const maxCORSItems = 256
const maxCORSHeaderBytes = 8192
const maxCORSPaths = 64

type corsPolicy struct {
	origins                                       map[Origin]bool
	patterns                                      []originMatcher
	paths                                         []corsPathPolicy
	methods                                       map[Method]bool
	headers                                       map[string]bool
	anyOrigin, anyMethod, anyHeaders, credentials bool
	expose                                        string
	maxAge                                        time.Duration
}

func (c CORSConfig) Validate() error { _, err := compileCORS(c); return err }

type corsPathPolicy struct {
	prefix string
	policy corsPolicy
}

func compileCORS(c CORSConfig) (corsPolicy, error) {
	policy, err := compileCORSPolicy(c)
	if err != nil {
		return corsPolicy{}, err
	}
	if len(c.Paths) > maxCORSPaths {
		return corsPolicy{}, fault.New(fault.Invalid, "CORS path policies exceed their declaration bound")
	}
	seen := make(map[string]bool, len(c.Paths))
	for _, declared := range c.Paths {
		prefix := declared.Prefix
		if prefix == "" || prefix[0] != '/' || len(prefix) > 1024 || prefix != "/" && strings.HasSuffix(prefix, "/") || path.Clean(prefix) != prefix || strings.ContainsAny(prefix, "?#*{}\\") {
			return corsPolicy{}, fault.New(fault.Invalid, "CORS path prefix must be a clean absolute path without a trailing slash, query or pattern")
		}
		if seen[prefix] {
			return corsPolicy{}, fault.New(fault.Duplicate, "CORS path prefix is repeated")
		}
		seen[prefix] = true
		if len(declared.Policy.Paths) != 0 {
			return corsPolicy{}, fault.New(fault.Invalid, "CORS path policies cannot declare nested paths")
		}
		compiled, err := compileCORSPolicy(declared.Policy)
		if err != nil {
			return corsPolicy{}, err
		}
		policy.paths = append(policy.paths, corsPathPolicy{prefix: prefix, policy: compiled})
	}
	return policy, nil
}

// forPath returns the longest matching path policy, or the enclosing policy.
func (p corsPolicy) forPath(requestPath string) corsPolicy {
	selected, length := p, -1
	for _, candidate := range p.paths {
		prefix := candidate.prefix
		if len(prefix) > length && (requestPath == prefix || prefix == "/" || strings.HasPrefix(requestPath, prefix) && requestPath[len(prefix)] == '/') {
			selected, length = candidate.policy, len(prefix)
		}
	}
	return selected
}

func compileCORSPolicy(c CORSConfig) (corsPolicy, error) {
	if len(c.Origins) > maxCORSItems || len(c.OriginPatterns) > maxCORSItems || len(c.Methods) > maxCORSItems || len(c.Headers) > maxCORSItems || len(c.ExposeHeaders) > maxCORSItems {
		return corsPolicy{}, fault.New(fault.Invalid, "CORS declaration exceeds its item bound")
	}
	if c.AnyOrigin && (len(c.Origins) != 0 || len(c.OriginPatterns) != 0 || c.Credentials) || c.AnyMethod && len(c.Methods) != 0 || c.AnyHeaders && len(c.Headers) != 0 {
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
	patterns := make(map[originMatcher]bool, len(c.OriginPatterns))
	for _, item := range c.OriginPatterns {
		matcher, err := compileOriginPattern(item)
		if err != nil {
			return corsPolicy{}, err
		}
		if patterns[matcher] {
			return corsPolicy{}, fault.New(fault.Duplicate, "CORS origin pattern is repeated")
		}
		patterns[matcher] = true
		policy.patterns = append(policy.patterns, matcher)
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
