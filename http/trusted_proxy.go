package http

import (
	"context"
	stdhttp "net/http"
	"net/netip"

	"github.com/weiloon1234/Foundry-Go/attribution"
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
// Invalid trusted-source data produces a shared 400 response, with no fallback
// to a lower-priority header. Unknown/obfuscated Forwarded hops stop traversal.
func TrustedProxy(config TrustedProxyConfig) Middleware {
	policy, err := compileTrustedProxy(config)
	return defineReplayMiddleware(TrustedProxyMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			ip, err := policy.clientIP(r)
			if err != nil {
				writeRoutingError(w, r, err)
				return
			}
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
}

func (p proxyPolicy) clientIP(r *stdhttp.Request) (netip.Addr, error) {
	peer := PeerIP(r)
	if !p.trusts(peer) {
		return peer, nil
	}
	for _, source := range p.headers {
		values := r.Header.Values(string(source.name))
		if len(values) == 0 {
			continue
		}
		chain, err := parseProxyAddresses(source, values)
		if err != nil {
			return netip.Addr{}, err
		}
		ip := peer
		for i := len(chain) - 1; i >= 0; i-- {
			if !p.trusts(ip) || !chain[i].IsValid() {
				break
			}
			ip = chain[i]
		}
		return ip, nil
	}
	return peer, nil
}
