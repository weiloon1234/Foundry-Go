package http

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PublicURLConfig allows exact public request origins. Origins include scheme
// and port; no host wildcard or incoming Host is implicitly trusted. Canonical,
// when set, must be allowed and supplies generated URLs for all accepted aliases.
// It does not redirect requests or change IsSecure's transport determination.
type PublicURLConfig struct {
	AllowedOrigins []Origin
	Canonical      value.Optional[Origin]
}

const PublicURLMiddlewareID MiddlewareID = "foundry.public-url"
const maxPublicOrigins = 256

type publicURLPolicy struct {
	allowed   map[Origin]bool
	canonical Origin
}

func (c PublicURLConfig) Validate() error { _, err := compilePublicURLs(c); return err }

func compilePublicURLs(c PublicURLConfig) (publicURLPolicy, error) {
	if len(c.AllowedOrigins) == 0 || len(c.AllowedOrigins) > maxPublicOrigins {
		return publicURLPolicy{}, fault.New(fault.Invalid, "public URLs require bounded allowed origins")
	}
	p := publicURLPolicy{allowed: make(map[Origin]bool, len(c.AllowedOrigins))}
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
	if origin, ok := c.Canonical.Get(); ok {
		canonical, err := ParseOrigin(string(origin))
		if err != nil || !p.allowed[canonical] {
			return publicURLPolicy{}, fault.New(fault.Invalid, "canonical public URL origin must be allowed")
		}
		p.canonical = canonical
	}
	return p, nil
}

type publicURLContextKey struct{}

// PublicURLs validates the request origin and binds an approved base for typed
// route URL generation. Run after TrustedProxy when using forwarded authorities.
// Unknown/malformed origins return a shared 400 before handlers execute. This
// middleware leaves native Host, URL, TLS, headers, writer and routing unchanged.
func PublicURLs(config PublicURLConfig) Middleware {
	policy, err := compilePublicURLs(config)
	return defineReplayMiddleware(PublicURLMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			origin, err := requestOrigin(r)
			if err != nil || !policy.allowed[origin] {
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
	if len(relative) == 0 || len(relative) > 64<<10 || !strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, "//") || strings.ContainsAny(relative, "\\#") {
		return "", fault.New(fault.Invalid, "public URL requires a bounded origin-relative path")
	}
	for i := range len(relative) {
		if relative[i] <= ' ' || relative[i] == 127 {
			return "", fault.New(fault.Invalid, "public URL contains invalid whitespace or control bytes")
		}
	}
	u, err := url.ParseRequestURI(relative)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" || u.User != nil {
		return "", fault.New(fault.Invalid, "invalid origin-relative URL")
	}
	if _, err := url.QueryUnescape(u.RawQuery); err != nil {
		return "", fault.New(fault.Invalid, "invalid origin-relative query escaping")
	}
	return string(base) + relative, nil
}
