package http

import (
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
)

const CompressionMiddlewareID MiddlewareID = "foundry.compression"

// Compression negotiates gzip/Brotli and streams through bounded prefix buffers.
// It does not decompress request bodies. Existing encodings, range responses,
// no-transform, private/no-store, Set-Cookie, SSE and gRPC are not transformed.
// Full-duplex activation before encoding preserves native streaming. Observed
// source/write failures abort instead of finalizing an incomplete encoded body.
// HEAD omits unknown representation lengths; upgrades retain the native writer.
// A malformed Accept-Encoding field is ignored and the response is not encoded.
func Compression(config CompressionConfig) Middleware {
	config = config.clone()
	err := config.Validate()
	return defineReplayMiddleware(CompressionMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		permits := admission.New(config.MaxConcurrent)
		pools := newCompressionPools(config.Encoders)
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			preferences, err := parseAcceptEncoding(r.Header.Values("Accept-Encoding"))
			if err != nil {
				// Accept-Encoding is client-owned negotiation metadata. A malformed,
				// duplicated or oversized field is ignored: identity is always sent.
				preferences = compressionPreferences{}
			}
			if r.Header.Get("Upgrade") != "" || strings.EqualFold(r.Header.Get("Connection"), "upgrade") {
				appendVary(w.Header(), "Accept-Encoding")
				next.ServeHTTP(w, r)
				return
			}
			response := &compressionResponse{underlying: w, request: r, header: w.Header().Clone(), config: config, preferences: preferences, permits: permits, pools: pools, declaredLength: -1}
			defer response.release()
			next.ServeHTTP(responseCapabilities(response), r)
			if err := response.finish(); err != nil {
				logRouteFailure(r, "HTTP compression could not finish", fault.Wrap(fault.Internal, "response transfer failed", err))
				panic(stdhttp.ErrAbortHandler)
			}
		}), nil
	})
}
