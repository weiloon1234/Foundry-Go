package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	stdhttp "net/http"
)

const ETagMiddlewareID MiddlewareID = "foundry.etag"

// ETags computes a validator for bounded, complete successful GET responses. HEAD and
// unsafe methods retain their native handler behavior. Streaming, overflow,
// existing validators, trailers, full-duplex I/O and saturated capture capacity pass through.
// A handler is executed once; this middleware does not cache response bodies.
func ETags(config ETagConfig) Middleware {
	err := config.Validate()
	return defineReplayMiddleware(ETagMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		permits := make(chan struct{}, config.MaxConcurrent)
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if r.Method != stdhttp.MethodGet || r.Header.Get("Upgrade") != "" || len(r.Header.Values("Range")) != 0 {
				next.ServeHTTP(w, r)
				return
			}
			select {
			case permits <- struct{}{}:
			default:
				next.ServeHTTP(w, r)
				return
			}
			response := &etagResponse{
				native: w, request: r, header: w.Header().Clone(),
				buffer: responseBuffer{limit: config.MaxBytes}, declared: -1,
				permits: permits, acquired: true,
			}
			defer response.release()
			next.ServeHTTP(responseCapabilities(response), r)
			if err := response.finish(); err != nil {
				logRouteFailure(r, "HTTP automatic ETag response could not finish", fault.Wrap(fault.Internal, "response transfer failed", err))
				panic(stdhttp.ErrAbortHandler)
			}
		}), nil
	})
}
