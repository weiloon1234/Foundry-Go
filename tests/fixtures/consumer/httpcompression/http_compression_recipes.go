package httpcompression

import (
	"net/http"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func compressedHandler(router http.Handler) (http.Handler, error) {
	// doc-example:begin http-compression-1
	return foundryhttp.ApplyMiddleware(
		router,
		foundryhttp.Compression(foundryhttp.DefaultCompressionConfig()),
	)
	// doc-example:end http-compression-1
}

func compressionConfig() foundryhttp.CompressionConfig {
	// doc-example:begin http-compression-2
	config := foundryhttp.DefaultCompressionConfig()
	config.Encoders = []foundryhttp.CompressionEncoder{
		foundryhttp.GzipCompression(foundryhttp.GzipFastest),
		foundryhttp.BrotliCompression(foundryhttp.BrotliQuality(4), foundryhttp.BrotliWindow(20)),
	}
	// doc-example:end http-compression-2
	return config
}

func compressedETags(router http.Handler) (http.Handler, error) {
	// doc-example:begin http-compression-3
	return foundryhttp.ApplyMiddleware(
		router,
		foundryhttp.ETags(foundryhttp.DefaultETagConfig()),
		foundryhttp.Compression(foundryhttp.DefaultCompressionConfig()),
	)
	// doc-example:end http-compression-3
}
