package httpcompression

import (
	"net/http"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin http-compression-1
func compressedHandler(router http.Handler) (http.Handler, error) {
	return foundryhttp.ApplyMiddleware(
		router,
		foundryhttp.Compression(foundryhttp.DefaultCompressionConfig()),
	)
}

// doc-example:end http-compression-1

// doc-example:begin http-compression-2
func compressionConfig() foundryhttp.CompressionConfig {
	config := foundryhttp.DefaultCompressionConfig()
	config.Encoders = []foundryhttp.CompressionEncoder{
		foundryhttp.GzipCompression(foundryhttp.GzipFastest),
		foundryhttp.BrotliCompression(foundryhttp.BrotliQuality(4), foundryhttp.BrotliWindow(20)),
	}
	return config
}

// doc-example:end http-compression-2

// doc-example:begin http-compression-3
func compressedETags(router http.Handler) (http.Handler, error) {
	return foundryhttp.ApplyMiddleware(
		router,
		foundryhttp.ETags(foundryhttp.DefaultETagConfig()),
		foundryhttp.Compression(foundryhttp.DefaultCompressionConfig()),
	)
}

// doc-example:end http-compression-3
