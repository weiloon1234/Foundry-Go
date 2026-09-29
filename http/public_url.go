package http

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PublicURLConfig allows public request origins. Origins include scheme and
// port; no incoming Host is implicitly trusted. AllowedPatterns admits HTTP(S)
// subdomain wildcards such as https://*.example.com for tenant hosts; the
// admitted request origin itself becomes the URL base. Canonical, when set,
// must be allowed and supplies generated URLs for all accepted aliases. It does
// not redirect requests or change IsSecure's transport determination.
// ExemptPaths lists exact request paths, such as load-balancer or Kubernetes
// probes that arrive with Host: <pod-ip>, served without host admission. No
// public origin is bound for them, so they cannot generate public/signed URLs.
type PublicURLConfig struct {
	AllowedOrigins  []Origin
	AllowedPatterns []OriginPattern
	Canonical       value.Optional[Origin]
	ExemptPaths     []string
}

const PublicURLMiddlewareID MiddlewareID = "foundry.public-url"

// PublicURLRequiredMiddlewareID marks a route that generates public URLs.
const PublicURLRequiredMiddlewareID MiddlewareID = "foundry.public-url-required"

const maxPublicOrigins = 256
const maxPublicExemptPaths = 64

type publicURLPolicy struct {
	allowed   map[Origin]bool
	patterns  []originMatcher
	exempt    map[string]bool
	canonical Origin
}

func (c PublicURLConfig) Validate() error { _, err := compilePublicURLs(c); return err }

func compilePublicURLs(c PublicURLConfig) (publicURLPolicy, error) {
	if len(c.AllowedOrigins)+len(c.AllowedPatterns) == 0 || len(c.AllowedOrigins) > maxPublicOrigins || len(c.AllowedPatterns) > maxPublicOrigins {
		return publicURLPolicy{}, fault.New(fault.Invalid, "public URLs require bounded allowed origins")
	}
	p := publicURLPolicy{allowed: make(map[Origin]bool, len(c.AllowedOrigins)), exempt: make(map[string]bool, len(c.ExemptPaths))}
	for _, input := range c.AllowedOrigins {
		origin, err := ParseOrigin(string(input))
		if err != nil || origin == NullOrigin {
			return publicURLPolicy{}, fault.New(fault.Invalid, "public URL origin requires an HTTP(S) authority")
		}
		if p.allowed[origin] {
			return publicURLPolicy{}, fault.New(fault.Duplicate, "public URL origin is repeated")
		}
		p.allowed[origin] = true
	}
	for _, input := range c.AllowedPatterns {
		matcher, err := compileOriginPattern(input)
		if err != nil || matcher.exact != "" {
			return publicURLPolicy{}, fault.New(fault.Invalid, "public URL patterns require an HTTP(S) subdomain wildcard")
		}
		if slices.Contains(p.patterns, matcher) {
			return publicURLPolicy{}, fault.New(fault.Duplicate, "public URL pattern is repeated")
		}
		p.patterns = append(p.patterns, matcher)
	}
	if len(c.ExemptPaths) > maxPublicExemptPaths {
		return publicURLPolicy{}, fault.New(fault.Invalid, "public URL exempt paths exceed their declaration bound")
	}
	for _, exempt := range c.ExemptPaths {
		if exempt == "" || exempt[0] != '/' || len(exempt) > 1024 || path.Clean(exempt) != exempt || strings.ContainsAny(exempt, "?#*{}\\ ") {
			return publicURLPolicy{}, fault.New(fault.Invalid, "public URL exempt paths must be exact clean absolute paths")
		}
		if p.exempt[exempt] {
			return publicURLPolicy{}, fault.New(fault.Duplicate, "public URL exempt path is repeated")
		}
		p.exempt[exempt] = true
	}
	if origin, ok := c.Canonical.Get(); ok {
		canonical, err := ParseOrigin(string(origin))
		if err != nil || !p.admits(canonical) {
			return publicURLPolicy{}, fault.New(fault.Invalid, "canonical public URL origin must be allowed")
		}
		p.canonical = canonical
	}
	return p, nil
}

func (p publicURLPolicy) admits(origin Origin) bool {
	if origin == NullOrigin || origin == "" {
		return false
	}
	if p.allowed[origin] {
		return true
	}
	for _, pattern := range p.patterns {
		if pattern.matches(string(origin)) {
			return true
		}
	}
	return false
}

type publicURLContextKey struct{}

// PublicURLs validates the request origin and binds an approved base for typed
// route URL generation. Run after TrustedProxy when using forwarded authorities;
// ApplyMiddleware rejects the reverse order. Unknown/malformed origins return a
// shared 400 before handlers execute, except on ExemptPaths. This middleware
// leaves native Host, URL, TLS, headers, writer and routing unchanged.
func PublicURLs(config PublicURLConfig) Middleware {
	policy, err := compilePublicURLs(config)
	return defineReplayMiddleware(PublicURLMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if len(policy.exempt) != 0 && r.URL != nil && policy.exempt[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			origin, err := requestOrigin(r)
			if err != nil || !policy.admits(origin) {
				writeRoutingError(w, r, BadRequest)
				return
			}
			if policy.canonical != "" {
				origin = policy.canonical
			}
			ctx := context.WithValue(r.Context(), publicURLContextKey{}, origin)
			next.ServeHTTP(w, r.WithContext(ctx))
		}), nil
	})
}

// RequirePublicURLs declares that a route generates public URLs. Place it on
// the route, inside any route-level PublicURLs. ApplyMiddleware rejects a router
// whose marked or signed routes are not covered by PublicURLs in the same chain
// or on the route; a router served without ApplyMiddleware fails the request
// with a shared 500 instead of inferring an origin from the request Host.
func RequirePublicURLs() Middleware {
	return defineReplayMiddleware(PublicURLRequiredMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if _, ok := PublicOrigin(r.Context()); !ok {
				writeRoutingError(w, r, InternalError.WithCause(fault.New(fault.Missing, "route requires PublicURLs middleware")))
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}

// validatePublicURLRequirements checks a router's routes that generate public
// URLs (signed routes and RequirePublicURLs routes) against the chain applied
// directly around it. Other handlers carry no route metadata to inspect.
func validatePublicURLRequirements(handler stdhttp.Handler, chain []Middleware) error {
	router, ok := handler.(*Router)
	if !ok || router == nil {
		return nil
	}
	for _, item := range chain {
		if item.id == PublicURLMiddlewareID {
			return nil
		}
	}
	for _, route := range router.Routes() {
		requires := route.SignedURL != nil || slices.Contains(route.Middlewares, PublicURLRequiredMiddlewareID)
		if requires && !slices.Contains(route.Middlewares, PublicURLMiddlewareID) {
			return fault.New(fault.Invalid, "route "+string(route.ID)+" generates public URLs; apply PublicURLs in the same ApplyMiddleware chain as the router or on the route")
		}
	}
	return nil
}

// PublicOrigin returns the approved (possibly canonical) URL base. Its absence
// is explicit: the framework never falls back to an arbitrary request Host.
func PublicOrigin(ctx context.Context) (Origin, bool) {
	if ctx == nil {
		return "", false
	}
	origin, ok := ctx.Value(publicURLContextKey{}).(Origin)
	return origin, ok && origin != ""
}

// PublicURL combines the request's approved origin with a relative URL returned
// by a typed route or endpoint. Background kernels use AbsoluteURL with an
// explicitly configured Origin. No request Host or environment global is inferred.
func PublicURL(ctx context.Context, relative string) (string, error) {
	origin, ok := PublicOrigin(ctx)
	if !ok {
		return "", fault.New(fault.Missing, "public URL origin is not bound to this context")
	}
	return AbsoluteURL(origin, relative)
}

// AbsoluteURL accepts an origin-relative path/query and an explicit HTTP(S)
// origin. Absolute/network-path URLs, fragments, backslashes, control bytes and
// invalid escaping are rejected. Existing route path/query escaping is preserved.
func AbsoluteURL(origin Origin, relative string) (string, error) {
	base, err := ParseOrigin(string(origin))
	if err != nil || base == NullOrigin {
		return "", fault.New(fault.Invalid, "absolute URL requires an HTTP(S) origin")
	}
	if err := validateRelativeURL(relative); err != nil {
		return "", err
	}
	return string(base) + relative, nil
}

// validateRelativeURL accepts one bounded origin-relative path/query.
func validateRelativeURL(relative string) error {
	if len(relative) == 0 || len(relative) > 64<<10 || !strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, "//") || strings.ContainsAny(relative, "\\#") {
		return fault.New(fault.Invalid, "public URL requires a bounded origin-relative path")
	}
	for i := range len(relative) {
		if relative[i] <= ' ' || relative[i] == 127 {
			return fault.New(fault.Invalid, "public URL contains invalid whitespace or control bytes")
		}
	}
	u, err := url.ParseRequestURI(relative)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" || u.User != nil {
		return fault.New(fault.Invalid, "invalid origin-relative URL")
	}
	if _, err := url.QueryUnescape(u.RawQuery); err != nil {
		return fault.New(fault.Invalid, "invalid origin-relative query escaping")
	}
	return nil
}
