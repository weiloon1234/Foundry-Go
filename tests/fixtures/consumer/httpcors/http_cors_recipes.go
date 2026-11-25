package httpcors

import (
	"net/http"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin http-cors-1
func browserHandler(router http.Handler) (http.Handler, error) {
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.CORS(foundryhttp.CORSConfig{
		Origins:       []foundryhttp.Origin{"https://console.example.test"},
		Methods:       []foundryhttp.Method{foundryhttp.PATCH},
		Headers:       []foundryhttp.HeaderName{"Content-Type", "Authorization"},
		ExposeHeaders: []foundryhttp.HeaderName{"X-Request-Id"},
		Credentials:   true,
		MaxAge:        10 * time.Minute,
	}))
	return handler, err
}

// doc-example:end http-cors-1

// doc-example:begin http-cors-2
func browserOrigins() foundryhttp.CORSConfig {
	return foundryhttp.CORSConfig{
		Origins:        []foundryhttp.Origin{"https://console.example.com"},
		OriginPatterns: []foundryhttp.OriginPattern{"capacitor://localhost", "https://*.tenants.example.com"},
		Methods:        []foundryhttp.Method{foundryhttp.PATCH},
	}

}

// doc-example:end http-cors-2

// doc-example:begin http-cors-3
func publicPathPolicy() foundryhttp.CORSConfig {
	return foundryhttp.CORSConfig{
		Origins: []foundryhttp.Origin{"https://console.example.com"},
		Paths: []foundryhttp.CORSPath{
			{Prefix: "/public", Policy: foundryhttp.CORSConfig{AnyOrigin: true, AnyMethod: true}},
		},
	}

}

// doc-example:end http-cors-3
