package httpproxy

import (
	"context"
	"net/http"
	"net/netip"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin http-trusted-proxy-1
func proxyHandler(router http.Handler) (http.Handler, error) {
	handler, err := foundryhttp.ApplyMiddleware(router,
		foundryhttp.TrustedProxy(foundryhttp.TrustedProxyConfig{
			Proxies: []netip.Prefix{
				netip.MustParsePrefix("10.20.0.0/24"),
			},
			Headers: []foundryhttp.ProxyHeader{
				foundryhttp.ForwardedHeader(),
				foundryhttp.XForwardedForHeader(),
			},
		}),
	)
	return handler, err
}

// doc-example:end http-trusted-proxy-1

// doc-example:begin http-trusted-proxy-2
func clientAddress(ctx context.Context) netip.Addr {
	client := foundryhttp.ClientIP(ctx) // netip.Addr from the service's context
	return client
}

// doc-example:end http-trusted-proxy-2
