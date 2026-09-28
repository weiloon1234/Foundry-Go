package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	stdhttp "net/http"
	"net/netip"
	"strconv"
	"time"
)

// RateLimit admits one unit using a concrete resource key. The limiter owns key
// resolution, its deadline and concurrency slot. Backend failures return the shared
// 503 error; confirmed quota exhaustion returns 429. Reusing a declaration shares
// quota across routes; use distinct declarations to scope quotas independently.
func RateLimit[K any](limiter ratelimit.Limiter[K], key func(*stdhttp.Request) (K, error)) Middleware {
	// Family names allow uppercase and 128 bytes, unlike MiddlewareID. A bounded
	// deterministic digest preserves their exact identity without normalization.
	digest := sha256.Sum256([]byte(limiter.Name()))
	id := MiddlewareID("foundry.rate-limit." + hex.EncodeToString(digest[:]))
	return defineReplayMiddleware(id, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := limiter.Validate(); err != nil {
			return nil, err
		}
		if key == nil {
			return nil, fault.New(fault.Invalid, "HTTP rate limiting requires a key resolver")
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var keyError error
			decision, err := limiter.TakeWith(r.Context(), 1, func(ctx context.Context) (K, error) {
				value, err := key(r.WithContext(ctx))
				keyError = err
				return value, err
			})
			if err != nil {
				if keyError != nil {
					writeRoutingError(w, r, keyError)
				} else {
					writeRoutingError(w, r, Unavailable.WithCause(err))
				}
				return
			}
			w.Header().Set("X-RateLimit-Limit", strconv.FormatUint(uint64(decision.Limit), 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatUint(uint64(decision.Remaining), 10))
			w.Header().Set("X-RateLimit-Reset", rateLimitSeconds(decision.ResetAfter))
			if !decision.Allowed {
				w.Header().Set("Retry-After", rateLimitSeconds(decision.RetryAfter))
				writeRoutingError(w, r, RateLimited)
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}

// RateLimitByIP uses Foundry attribution, falling back to the direct peer. Install
// TrustedProxy before this middleware when trusted forwarded attribution is needed.
// It never parses forwarded headers itself. IPs are unmapped and zone-free.
func RateLimitByIP(limiter ratelimit.Limiter[netip.Addr]) Middleware {
	return RateLimit(limiter, func(r *stdhttp.Request) (netip.Addr, error) {
		ip := ClientIP(r.Context())
		if !ip.IsValid() {
			ip = PeerIP(r)
		}
		if !ip.IsValid() {
			return netip.Addr{}, BadRequest
		}
		return ip.Unmap().WithZone(""), nil
	})
}
func rateLimitSeconds(duration time.Duration) string {
	seconds := duration / time.Second
	if duration%time.Second != 0 {
		seconds++
	}
	return strconv.FormatInt(int64(seconds), 10)
}
