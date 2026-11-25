package httpsecurity

import (
	"context"
	"net/http"
	"net/netip"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func publicHandler(router http.Handler, proxy foundryhttp.TrustedProxyConfig, security foundryhttp.SecurityHeadersConfig) (http.Handler, error) {
	// doc-example:begin http-public-urls-1
	public := foundryhttp.PublicURLConfig{
		AllowedOrigins: []foundryhttp.Origin{
			"https://app.example.test",
			"http://alias.example.test",
		},
		Canonical: value.Set(foundryhttp.Origin("https://app.example.test")),
	}
	handler, err := foundryhttp.ApplyMiddleware(router,
		foundryhttp.TrustedProxy(proxy),
		foundryhttp.PublicURLs(public),
		foundryhttp.SecurityHeaders(security),
	)
	// doc-example:end http-public-urls-1
	return handler, err
}

func tenantOrigins() foundryhttp.PublicURLConfig {
	// doc-example:begin http-public-urls-2
	public := foundryhttp.PublicURLConfig{
		AllowedOrigins:  []foundryhttp.Origin{"https://app.example.com"},
		AllowedPatterns: []foundryhttp.OriginPattern{"https://*.tenants.example.com"},
		ExemptPaths:     []string{"/healthz", "/readyz"},
	}
	// doc-example:end http-public-urls-2
	return public
}

func absoluteURL(ctx context.Context, endpoint foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent], path foundryhttp.NoPath, query foundryhttp.NoQuery) (string, error) {
	// doc-example:begin http-public-urls-3
	relative, err := endpoint.URL(ctx, path, query)
	if err != nil {
		return "", err
	}
	return foundryhttp.PublicURL(ctx, relative)
	// doc-example:end http-public-urls-3
}

func originProxy() foundryhttp.TrustedProxyConfig {
	// doc-example:begin http-public-urls-4
	proxy := foundryhttp.TrustedProxyConfig{
		Proxies: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
		Headers: []foundryhttp.ProxyHeader{foundryhttp.ForwardedHeader()},
		OriginHeaders: []foundryhttp.ProxyOriginHeader{
			foundryhttp.ForwardedOriginHeader(),
		},
	}
	// doc-example:end http-public-urls-4
	return proxy
}
