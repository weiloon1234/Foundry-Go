// Package httpsecurity applies typed response policy to existing endpoints.
package httpsecurity

import (
	"net/http"
	"time"

	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpproxy"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func Handler(service httpendpoints.Service) (http.Handler, error) {
	router, err := httpproxy.Handler(service)
	if err != nil {
		return nil, err
	}
	config := foundryhttp.DefaultSecurityHeadersConfig()
	config.Frame = foundryhttp.FrameSameOrigin
	config.HSTS = value.Set(foundryhttp.HSTSPolicy{MaxAge: 24 * time.Hour})
	config.Extra = []foundryhttp.ResponseHeader{{Name: "X-Build", Value: "fixture"}}
	return foundryhttp.ApplyMiddleware(router, foundryhttp.SecurityHeaders(config))
}
