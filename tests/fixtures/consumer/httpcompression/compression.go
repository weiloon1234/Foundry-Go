// Package httpcompression composes framework compression around an existing
// typed endpoint; domain services do not implement encoding or writer plumbing.
package httpcompression

import (
	"net/http"

	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func Handler(service httpendpoints.Service) (http.Handler, error) {
	router, err := httpendpoints.Router(service)
	if err != nil {
		return nil, err
	}
	config := foundryhttp.DefaultCompressionConfig()
	return foundryhttp.ApplyMiddleware(router, foundryhttp.Compression(config))
}

// EncodingNames lets inspection retain typed coding identifiers.
func EncodingNames() []foundryhttp.ContentEncoding {
	config := foundryhttp.DefaultCompressionConfig()
	names := make([]foundryhttp.ContentEncoding, len(config.Encoders))
	for i, encoder := range config.Encoders {
		names[i] = encoder.Encoding()
	}
	return names
}
