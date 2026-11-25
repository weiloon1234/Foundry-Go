package httpcors

import (
	"net/http"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func browserHandler(router http.Handler) (http.Handler, error) {
	// doc-example:begin http-cors-1
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.CORS(foundryhttp.CORSConfig{
		Origins:       []foundryhttp.Origin{"https://console.example.test"},
		Methods:       []foundryhttp.Method{foundryhttp.PATCH},
		Headers:       []foundryhttp.HeaderName{"Content-Type", "Authorization"},
		ExposeHeaders: []foundryhttp.HeaderName{"X-Request-Id"},
		Credentials:   true,
		MaxAge:        10 * time.Minute,
	}))
	// doc-example:end http-cors-1
	return handler, err
}

func browserOrigins() foundryhttp.CORSConfig {
	// doc-example:begin http-cors-2
	config := foundryhttp.CORSConfig{
		Origins:        []foundryhttp.Origin{"https://console.example.com"},
		OriginPatterns: []foundryhttp.OriginPattern{"capacitor://localhost", "https://*.tenants.example.com"},
		Methods:        []foundryhttp.Method{foundryhttp.PATCH},
	}
	// doc-example:end http-cors-2
	return config
}

func publicPathPolicy() foundryhttp.CORSConfig {
	// doc-example:begin http-cors-3
	config := foundryhttp.CORSConfig{
		Origins: []foundryhttp.Origin{"https://console.example.com"},
		Paths: []foundryhttp.CORSPath{
			{Prefix: "/public", Policy: foundryhttp.CORSConfig{AnyOrigin: true, AnyMethod: true}},
		},
	}
	// doc-example:end http-cors-3
	return config
}
