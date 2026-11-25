package httpproxy

import (
	"context"
	"net/http"
	"net/netip"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func proxyHandler(router http.Handler) (http.Handler, error) {
	// doc-example:begin http-trusted-proxy-1
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
	// doc-example:end http-trusted-proxy-1
	return handler, err
}

func clientAddress(ctx context.Context) netip.Addr {
	// doc-example:begin http-trusted-proxy-2
	return foundryhttp.ClientIP(ctx) // netip.Addr from the service's context
	// doc-example:end http-trusted-proxy-2
}
