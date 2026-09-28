// Package httpproxy resolves client attribution before invoking typed services.
package httpproxy

import (
	"net/http"
	"net/netip"

	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpmiddleware"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func Handler(service httpendpoints.Service) (http.Handler, error) {
	router, err := httpmiddleware.Router(service)
	if err != nil {
		return nil, err
	}
	return foundryhttp.ApplyMiddleware(router, foundryhttp.TrustedProxy(foundryhttp.TrustedProxyConfig{
		// These are fixture peers only. A deployed app declares its actual proxies.
		Proxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		Headers: []foundryhttp.ProxyHeader{foundryhttp.ForwardedHeader(), foundryhttp.XForwardedForHeader()},
	}))
}
