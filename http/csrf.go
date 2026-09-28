package http

import (
	stdhttp "net/http"
	"net/url"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const CSRFMiddlewareID MiddlewareID = "foundry.csrf"

// CSRFConfig lists exact additional origins trusted to submit browser requests.
// Zero configuration trusts only same-origin requests. Unsafe requests without
// Fetch Metadata or Origin evidence fail closed. Configure bearer API routes
// separately; CORS permission does not automatically grant CSRF trust.
type CSRFConfig struct {
	TrustedOrigins []Origin `config:",json"`
}

func (c CSRFConfig) Validate() error { _, err := compileCSRF(c); return err }

type csrfPolicy struct {
	native  *stdhttp.CrossOriginProtection
	trusted map[Origin]bool
}

func compileCSRF(c CSRFConfig) (csrfPolicy, error) {
	if len(c.TrustedOrigins) > maxPublicOrigins {
		return csrfPolicy{}, fault.New(fault.Invalid, "too many CSRF trusted origins")
	}
	p := csrfPolicy{native: stdhttp.NewCrossOriginProtection(), trusted: make(map[Origin]bool, len(c.TrustedOrigins))}
	for _, input := range c.TrustedOrigins {
		origin, err := ParseOrigin(string(input))
		if err != nil || origin == NullOrigin {
			return csrfPolicy{}, fault.New(fault.Invalid, "CSRF trust requires an HTTP(S) origin")
		}
		if p.trusted[origin] {
			return csrfPolicy{}, fault.New(fault.Duplicate, "CSRF trusted origin is repeated")
		}
		p.trusted[origin] = true
		if err := p.native.AddTrustedOrigin(string(origin)); err != nil {
			return csrfPolicy{}, fault.Wrap(fault.Invalid, "invalid CSRF trusted origin", err)
		}
	}
	return p, nil
}
func safeBrowserMethod(method string) bool {
	return method == stdhttp.MethodGet || method == stdhttp.MethodHead || method == stdhttp.MethodOptions
}

// CSRF uses Go's native cross-origin checks, bounded header parsing, and a
// scheme-aware Origin fallback. It preserves the native response writer. Run
// TrustedProxy first when the public scheme/host is supplied by a trusted edge.
func CSRF(config CSRFConfig) Middleware {
	policy, err := compileCSRF(config)
	return defineReplayMiddleware(CSRFMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			csrfVary(w.Header())
			if err := policy.check(r); err != nil {
				writeRoutingError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}
func csrfVary(header stdhttp.Header) { appendVary(header, "Sec-Fetch-Site", "Origin") }
func (p csrfPolicy) check(r *stdhttp.Request) error {
	if r == nil || p.native == nil {
		return fault.New(fault.Invalid, "CSRF requires a policy and request")
	}
	if err := r.Context().Err(); err != nil {
		return err
	}
	if safeBrowserMethod(r.Method) {
		return nil
	}
	origins, sites := r.Header.Values("Origin"), r.Header.Values("Sec-Fetch-Site")
	if len(origins) > 1 || len(sites) > 1 {
		return Forbidden
	}
	var site string
	if len(sites) == 1 {
		site = sites[0]
		switch site {
		case "same-origin", "same-site", "cross-site", "none":
		default:
			return Forbidden
		}
	}
	var origin Origin
	if len(origins) == 1 {
		var err error
		origin, err = ParseOrigin(origins[0])
		if err != nil || origin == NullOrigin {
			return Forbidden
		}
	}
	if site == "" && origin == "" {
		return Forbidden
	}
	target, err := requestOrigin(r)
	if err != nil {
		return Forbidden.WithCause(err)
	}
	// The standard library's legacy fallback compares only hosts. Enforce scheme
	// as well and reject contradictory Origin evidence even with Fetch Metadata.
	if origin != "" && origin != target && !p.trusted[origin] {
		return Forbidden
	}
	request := *r
	request.Header = r.Header.Clone()
	if origin != "" {
		request.Header.Set("Origin", string(origin))
	}
	public, _ := url.Parse(string(target))
	request.Host = public.Host
	if err := p.native.Check(&request); err != nil {
		return Forbidden.WithCause(err)
	}
	return nil
}
