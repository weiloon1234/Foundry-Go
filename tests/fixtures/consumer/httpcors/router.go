// Package httpcors applies typed browser sharing to a generated endpoint router.
package httpcors

import (
	"net/http"
	"time"

	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpmiddleware"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

const BrowserOrigin foundryhttp.Origin = "https://console.example.test"

func Handler(service httpendpoints.Service) (http.Handler, error) {
	router, err := httpmiddleware.Router(service)
	if err != nil {
		return nil, err
	}
	return foundryhttp.ApplyMiddleware(router, foundryhttp.CORS(foundryhttp.CORSConfig{
		Origins:       []foundryhttp.Origin{BrowserOrigin},
		Methods:       []foundryhttp.Method{foundryhttp.PATCH},
		Headers:       []foundryhttp.HeaderName{"Content-Type", "Authorization"},
		ExposeHeaders: []foundryhttp.HeaderName{"X-Matched-Route"},
		Credentials:   true,
		MaxAge:        10 * time.Minute,
	}))
}
