package httpetags

import (
	"net/http"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func conditionalHandler(router http.Handler) (http.Handler, error) {
	// doc-example:begin http-etags-1
	config := foundryhttp.DefaultETagConfig()
	return foundryhttp.ApplyMiddleware(router, foundryhttp.ETags(config))
	// doc-example:end http-etags-1
}

func cachedHandler(router http.Handler) (http.Handler, error) {
	// doc-example:begin http-etags-2
	policy := foundryhttp.CachePolicy{
		Visibility:           foundryhttp.CachePublic,
		MaxAge:               time.Minute,
		SharedMaxAge:         value.Set(5 * time.Minute), // s-maxage
		StaleWhileRevalidate: 30 * time.Second,
	}
	return foundryhttp.ApplyMiddleware(router,
		foundryhttp.CacheControl(policy),
		foundryhttp.ETags(foundryhttp.DefaultETagConfig()),
	)
	// doc-example:end http-etags-2
}
