package httpsecurity

import (
	"net/http"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func secureHandler(router http.Handler) (http.Handler, error) {
	// doc-example:begin http-security-headers-1
	config := foundryhttp.DefaultSecurityHeadersConfig()
	config.Frame = foundryhttp.FrameSameOrigin
	config.Referrer = foundryhttp.ReferrerNoReferrer
	config.HSTS = value.Set(foundryhttp.HSTSPolicy{
		MaxAge: 24 * time.Hour,
	})
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.SecurityHeaders(config))
	// doc-example:end http-security-headers-1
	return handler, err
}

func isolationPolicy(config foundryhttp.SecurityHeadersConfig) foundryhttp.SecurityHeadersConfig {
	// doc-example:begin http-security-headers-2
	config.CrossOriginOpener = foundryhttp.OpenerSameOrigin      // Cross-Origin-Opener-Policy
	config.CrossOriginEmbedder = foundryhttp.EmbedderRequireCORP // Cross-Origin-Embedder-Policy
	config.CrossOriginResource = foundryhttp.ResourceSameSite    // Cross-Origin-Resource-Policy
	config.Permissions = []foundryhttp.PermissionDirective{
		{Feature: foundryhttp.PermissionCamera}, // camera=()
		{Feature: foundryhttp.PermissionGeolocation, Self: true,
			Origins: []foundryhttp.Origin{"https://maps.example"}},
		{Feature: foundryhttp.PermissionFullscreen, Any: true}, // fullscreen=*
	}
	// doc-example:end http-security-headers-2
	return config
}

func buildHeader(config foundryhttp.SecurityHeadersConfig) foundryhttp.SecurityHeadersConfig {
	// doc-example:begin http-security-headers-3
	config.Extra = []foundryhttp.ResponseHeader{
		{Name: "X-Build", Value: "release-1"},
	}
	// doc-example:end http-security-headers-3
	return config
}
