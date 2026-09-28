package httpsecurity

import (
	"net/http"
	"net/netip"
	"time"

	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpmiddleware"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PublicHandler supplies an approved URL base to the existing typed endpoint.
func PublicHandler(service httpendpoints.Service) (http.Handler, error) {
	router, err := httpmiddleware.Router(service)
	if err != nil {
		return nil, err
	}
	proxy := foundryhttp.TrustedProxyConfig{
		// Loopback trust is specific to this independent test consumer.
		Proxies:       []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		Headers:       []foundryhttp.ProxyHeader{foundryhttp.XForwardedForHeader()},
		OriginHeaders: []foundryhttp.ProxyOriginHeader{foundryhttp.XForwardedOriginHeaders()},
	}
	public := foundryhttp.PublicURLConfig{
		AllowedOrigins: []foundryhttp.Origin{"https://app.example.test", "http://alias.example.test"},
		Canonical:      value.Set(foundryhttp.Origin("https://app.example.test")),
	}
	security := foundryhttp.DefaultSecurityHeadersConfig()
	security.HSTS = value.Set(foundryhttp.HSTSPolicy{MaxAge: 24 * time.Hour})
	return foundryhttp.ApplyMiddleware(router, foundryhttp.TrustedProxy(proxy), foundryhttp.PublicURLs(public), foundryhttp.SecurityHeaders(security))
}
