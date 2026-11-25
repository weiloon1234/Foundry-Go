package httpsecurity

import (
	"context"
	"net/http"
	"net/netip"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin http-public-urls-1
func publicHandler(router http.Handler, proxy foundryhttp.TrustedProxyConfig, security foundryhttp.SecurityHeadersConfig) (http.Handler, error) {
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
	return handler, err
}

// doc-example:end http-public-urls-1
// doc-example:begin http-public-urls-2
func tenantOrigins() foundryhttp.PublicURLConfig {
	public := foundryhttp.PublicURLConfig{
		AllowedOrigins:  []foundryhttp.Origin{"https://app.example.com"},
		AllowedPatterns: []foundryhttp.OriginPattern{"https://*.tenants.example.com"},
		ExemptPaths:     []string{"/healthz", "/readyz"},
	}
	return public
}

// doc-example:end http-public-urls-2
// doc-example:begin http-public-urls-3
func absoluteURL(ctx context.Context, endpoint foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent], path foundryhttp.NoPath, query foundryhttp.NoQuery) (string, error) {
	relative, err := endpoint.URL(ctx, path, query)
	if err != nil {
		return "", err
	}
	absolute, err := foundryhttp.PublicURL(ctx, relative)
	return absolute, err
}

// doc-example:end http-public-urls-3
// doc-example:begin http-public-urls-4
func originProxy() foundryhttp.TrustedProxyConfig {
	proxy := foundryhttp.TrustedProxyConfig{
		Proxies: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
		Headers: []foundryhttp.ProxyHeader{foundryhttp.ForwardedHeader()},
		OriginHeaders: []foundryhttp.ProxyOriginHeader{
			foundryhttp.ForwardedOriginHeader(),
		},
	}
	return proxy
}

// doc-example:end http-public-urls-4
