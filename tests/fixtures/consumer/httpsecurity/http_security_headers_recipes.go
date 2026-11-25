package httpsecurity

import (
	"net/http"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin http-security-headers-1
func secureHandler(router http.Handler) (http.Handler, error) {
	config := foundryhttp.DefaultSecurityHeadersConfig()
	config.Frame = foundryhttp.FrameSameOrigin
	config.Referrer = foundryhttp.ReferrerNoReferrer
	config.HSTS = value.Set(foundryhttp.HSTSPolicy{
		MaxAge: 24 * time.Hour,
	})
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.SecurityHeaders(config))
	return handler, err
}

// doc-example:end http-security-headers-1

// doc-example:begin http-security-headers-2
func isolationPolicy(config foundryhttp.SecurityHeadersConfig) foundryhttp.SecurityHeadersConfig {
	config.CrossOriginOpener = foundryhttp.OpenerSameOrigin      // Cross-Origin-Opener-Policy
	config.CrossOriginEmbedder = foundryhttp.EmbedderRequireCORP // Cross-Origin-Embedder-Policy
	config.CrossOriginResource = foundryhttp.ResourceSameSite    // Cross-Origin-Resource-Policy
	config.Permissions = []foundryhttp.PermissionDirective{
		{Feature: foundryhttp.PermissionCamera}, // camera=()
		{Feature: foundryhttp.PermissionGeolocation, Self: true,
			Origins: []foundryhttp.Origin{"https://maps.example"}},
		{Feature: foundryhttp.PermissionFullscreen, Any: true}, // fullscreen=*
	}
	return config
}

// doc-example:end http-security-headers-2

// doc-example:begin http-security-headers-3
func buildHeader(config foundryhttp.SecurityHeadersConfig) foundryhttp.SecurityHeadersConfig {
	config.Extra = []foundryhttp.ResponseHeader{
		{Name: "X-Build", Value: "release-1"},
	}
	return config
}

// doc-example:end http-security-headers-3
