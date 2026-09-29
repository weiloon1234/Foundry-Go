package http

import (
	"context"
	stdhttp "net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// ClientIP returns the IP captured in shared request attribution. The HTTP
// kernel starts with the socket peer; TrustedProxy may replace it after checking
// the peer and chain. Outside attributed work it returns an invalid address.
func ClientIP(ctx context.Context) netip.Addr { return attribution.FromContext(ctx).Request().IP }

// PeerIP parses the native socket peer without trusting any request header.
// IPv4-mapped addresses are unmapped and local interface zones are removed.
// A missing or non-IP peer returns an invalid address, never a trusted fallback.
func PeerIP(request *stdhttp.Request) netip.Addr {
	if request == nil {
		return netip.Addr{}
	}
	if peer, err := netip.ParseAddrPort(request.RemoteAddr); err == nil {
		return peer.Addr().Unmap().WithZone("")
	}
	if peer, err := netip.ParseAddr(request.RemoteAddr); err == nil {
		return peer.Unmap().WithZone("")
	}
	return netip.Addr{}
}

// TrustedProxy snapshots configuration and resolves client IP and declared origin metadata before invoking
// the next handler. Apply it globally before rate limits or authentication use
// request attribution. Native RemoteAddr, Host, URL, headers and TLS are unchanged.
// Unusable client-IP entries (unknown, obfuscated or malformed hops, including
// ip:port spellings it cannot parse) stop traversal at the last trusted hop with
// no fallback to a lower-priority header; they never reject the request. Invalid
// origin data at the trusted boundary still produces a shared 400 response.
func TrustedProxy(config TrustedProxyConfig) Middleware {
	policy, err := compileTrustedProxy(config)
	middleware := defineReplayMiddleware(TrustedProxyMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			ip := policy.clientIP(r)
			forwarded, err := policy.forwardedOrigin(r)
			if err != nil {
				writeRoutingError(w, r, err)
				return
			}
			origin := attribution.FromContext(r.Context())
			metadata := origin.Request()
			metadata.IP = ip
			origin, err = origin.WithRequest(metadata)
			if err != nil {
				writeRoutingError(w, r, InternalError.WithCause(err))
				return
			}
			ctx, err := attribution.WithContext(r.Context(), origin)
			if err != nil {
				writeRoutingError(w, r, InternalError.WithCause(err))
				return
			}
			ctx = context.WithValue(ctx, proxyOriginContextKey{}, forwarded)
			next.ServeHTTP(w, r.WithContext(ctx))
		}), nil
	})
	if err == nil {
		middleware.proxy = &policy
	}
	return middleware
}

// clientIP walks the first present source from the socket peer outward. Every
// consumed hop must be appended by a trusted peer; unknown, obfuscated and
// malformed entries stop the walk at the last trusted hop instead of failing the
// request. Lower-priority sources are never consulted once a source is present.
func (p proxyPolicy) clientIP(r *stdhttp.Request) netip.Addr {
	peer := PeerIP(r)
	if !p.trusts(peer) {
		return peer
	}
	for _, source := range p.headers {
		values := r.Header.Values(string(source.name))
		if len(values) == 0 {
			continue
		}
		ip := peer
		switch source.format {
		case proxySingleIP:
			// An overwritten single-IP field has exactly one value. Repeated or
			// list values are ambiguous, so the trusted peer remains the client.
			if len(values) == 1 {
				if hop := proxyHopAddress(values[0]); hop.IsValid() {
					ip = hop
				}
			}
		case proxyIPChain:
			reverseProxyEntries(values, func(entry string) bool {
				hop := proxyHopAddress(entry)
				if !hop.IsValid() {
					return false
				}
				ip = hop
				return p.trusts(ip)
			})
		case proxyForwardedChain:
			for _, hop := range forwardedHops(values) {
				if !hop.valid || !hop.address.IsValid() {
					break
				}
				ip = hop.address
				if !p.trusts(ip) {
					break
				}
			}
		}
		return ip
	}
	return peer
}

// proxyDependentMiddleware reports built-in policies that read TrustedProxy
// results: client IP (rate limits), public scheme/host (CSRF, PublicURLs,
// credential transport checks) or both (browser sessions).
func proxyDependentMiddleware(id MiddlewareID) bool {
	switch id {
	case CSRFMiddlewareID, PublicURLMiddlewareID, BrowserSessionMiddlewareID, CredentialRequestsMiddlewareID:
		return true
	}
	return strings.HasPrefix(string(id), rateLimitMiddlewarePrefix)
}

// validateRequestEdgePolicies runs when a chain is assembled around a handler.
// A proxy-dependent policy that executes before TrustedProxy would silently
// use the load balancer's address and scheme, so that ordering is rejected,
// both within one chain and between a router's global chain and its routes.
// Routes that generate public URLs must also be covered by PublicURLs.
func validateRequestEdgePolicies(handler stdhttp.Handler, chain []Middleware) error {
	ids := middlewareIDs(chain)
	if err := validateProxyOrdering(ids); err != nil {
		return err
	}
	if required := slices.Index(ids, PublicURLRequiredMiddlewareID); required >= 0 && slices.Index(ids, PublicURLMiddlewareID) > required {
		return fault.New(fault.Invalid, "PublicURLs must run before RequirePublicURLs in one middleware chain")
	}
	if router, ok := handler.(*Router); ok && router != nil {
		outer := slices.ContainsFunc(ids, proxyDependentMiddleware)
		for _, route := range router.Routes() {
			if err := validateProxyOrdering(route.Middlewares); err != nil {
				return err
			}
			if outer && slices.Contains(route.Middlewares, TrustedProxyMiddlewareID) {
				return fault.New(fault.Invalid, "route "+string(route.ID)+" installs TrustedProxy inside a global policy that depends on it; install TrustedProxy globally before rate limits, CSRF, PublicURLs and sessions")
			}
		}
	}
	return validatePublicURLRequirements(handler, chain)
}

func validateProxyOrdering(ids []MiddlewareID) error {
	proxy := slices.Index(ids, TrustedProxyMiddlewareID)
	if proxy < 0 {
		return nil
	}
	for _, id := range ids[:proxy] {
		if proxyDependentMiddleware(id) {
			return fault.New(fault.Invalid, "middleware "+string(id)+" runs before TrustedProxy and would use the proxy's address or scheme; install TrustedProxy first")
		}
	}
	return nil
}
