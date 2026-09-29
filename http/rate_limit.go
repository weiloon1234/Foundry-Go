package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	stdhttp "net/http"
	"net/netip"
	"strconv"
	"time"
)

// RateLimit admits one unit using a concrete resource key. The limiter owns key
// resolution, its deadline and concurrency slot. Backend failures return the shared
// 503 error; confirmed quota exhaustion returns 429 with Retry-After and the
// X-RateLimit-* headers. Reusing a declaration shares quota across routes; use
// distinct declarations to scope quotas independently.
func RateLimit[K any](limiter ratelimit.Limiter[K], key func(*stdhttp.Request) (K, error)) Middleware {
	return rateLimitMiddleware(limiter, key, nil)
}

func rateLimitMiddleware[K any](limiter ratelimit.Limiter[K], key func(*stdhttp.Request) (K, error), declaration error) Middleware {
	// Family names allow uppercase and 128 bytes, unlike MiddlewareID. A bounded
	// deterministic digest preserves their exact identity without normalization.
	digest := sha256.Sum256([]byte(limiter.Name()))
	id := MiddlewareID(rateLimitMiddlewarePrefix + hex.EncodeToString(digest[:]))
	return defineReplayMiddleware(id, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if declaration != nil {
			return nil, declaration
		}
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
			setRateLimitHeaders(w.Header(), decision)
			if !decision.Allowed {
				writeRoutingError(w, r, RateLimited)
				return
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}

// rateLimitMiddlewarePrefix identifies every built-in quota middleware, whose
// keys may depend on TrustedProxy attribution.
const rateLimitMiddlewarePrefix = "foundry.rate-limit."

// IPRateLimitOptions groups client addresses before the quota lookup. A single
// IPv6 subscriber usually controls at least a /64, so per-address IPv6 keys
// would let one client rotate through unlimited quotas. Prefix lengths are
// 1-32 for IPv4 and 1-128 for IPv6; zero values select the defaults.
type IPRateLimitOptions struct {
	IPv4Prefix int
	IPv6Prefix int
}

// DefaultIPRateLimitOptions keys IPv4 per address and IPv6 per /64 network.
func DefaultIPRateLimitOptions() IPRateLimitOptions {
	return IPRateLimitOptions{IPv4Prefix: 32, IPv6Prefix: 64}
}

func (o IPRateLimitOptions) Validate() error { _, err := o.compile(); return err }

func (o IPRateLimitOptions) compile() (IPRateLimitOptions, error) {
	defaults := DefaultIPRateLimitOptions()
	if o.IPv4Prefix == 0 {
		o.IPv4Prefix = defaults.IPv4Prefix
	}
	if o.IPv6Prefix == 0 {
		o.IPv6Prefix = defaults.IPv6Prefix
	}
	if o.IPv4Prefix < 1 || o.IPv4Prefix > 32 || o.IPv6Prefix < 1 || o.IPv6Prefix > 128 {
		return IPRateLimitOptions{}, fault.New(fault.Invalid, "IP rate-limit prefixes must be 1-32 for IPv4 and 1-128 for IPv6")
	}
	return o, nil
}

// key returns the masked network address used as the quota key.
func (o IPRateLimitOptions) key(ip netip.Addr) (netip.Addr, error) {
	compiled, err := o.compile()
	if err != nil {
		return netip.Addr{}, err
	}
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() {
		return netip.Addr{}, BadRequest
	}
	bits := compiled.IPv4Prefix
	if ip.Is6() {
		bits = compiled.IPv6Prefix
	}
	prefix, err := ip.Prefix(bits)
	if err != nil {
		return netip.Addr{}, BadRequest
	}
	return prefix.Addr(), nil
}

// RateLimitByIP uses Foundry attribution, falling back to the direct peer. IPv4
// is keyed per address and IPv6 per /64; use RateLimitByIPWith to change the
// grouping. Install TrustedProxy before (outside) this middleware when trusted
// forwarded attribution is needed; ApplyMiddleware rejects the reverse order.
// It never parses forwarded headers itself. IPs are unmapped and zone-free.
func RateLimitByIP(limiter ratelimit.Limiter[netip.Addr]) Middleware {
	return RateLimitByIPWith(limiter, DefaultIPRateLimitOptions())
}

// RateLimitByIPWith keys the quota by the client's masked network address.
func RateLimitByIPWith(limiter ratelimit.Limiter[netip.Addr], options IPRateLimitOptions) Middleware {
	compiled, err := options.compile()
	return rateLimitMiddleware(limiter, func(r *stdhttp.Request) (netip.Addr, error) {
		ip := ClientIP(r.Context())
		if !ip.IsValid() {
			ip = PeerIP(r)
		}
		return compiled.key(ip)
	}, err)
}

// RateLimitExceeded converts a denied decision from a handler-level limiter
// call into the shared 429 error. WriteError then sends Retry-After and the
// X-RateLimit-* headers from the decision. An allowed or invalid decision is an
// internal error: it must never be reported as a quota rejection.
func RateLimitExceeded(decision ratelimit.Decision) error {
	if decision.Allowed || decision.RetryAfter <= 0 || decision.Remaining > decision.Limit {
		return InternalError.WithCause(fault.New(fault.Invalid, "rate-limit rejection requires a denied decision"))
	}
	return &rateLimitRejection{decision: decision}
}

type rateLimitRejection struct{ decision ratelimit.Decision }

func (e *rateLimitRejection) Error() string            { return RateLimited.Error() }
func (e *rateLimitRejection) Is(target error) bool     { return target == RateLimited }
func (e *rateLimitRejection) httpErrorCode() ErrorCode { return RateLimited }

func setRateLimitHeaders(header stdhttp.Header, decision ratelimit.Decision) {
	header.Set("X-RateLimit-Limit", strconv.FormatUint(uint64(decision.Limit), 10))
	header.Set("X-RateLimit-Remaining", strconv.FormatUint(uint64(decision.Remaining), 10))
	header.Set("X-RateLimit-Reset", rateLimitSeconds(decision.ResetAfter))
	if !decision.Allowed {
		header.Set("Retry-After", rateLimitSeconds(max(decision.RetryAfter, time.Second)))
	}
}

// rateLimitedHeaders adds retry metadata carried by a 429 error: bounded lockout
// delays and handler-level RateLimitExceeded decisions. Arbitrary error-chain
// methods run in an owned callback within the shared traversal bounds.
func rateLimitedHeaders(header stdhttp.Header, err error) error {
	retry, failure := authenticationRetryAfter(err)
	if failure != nil {
		return failure
	}
	if retry > 0 {
		header.Set("Retry-After", rateLimitSeconds(retry))
		return nil
	}
	var decision ratelimit.Decision
	found := false
	failure = callback.Isolated("HTTP rate-limit retry classification", func() error {
		rejected, matched, complete := errorgraph.As[*rateLimitRejection](err)
		if !complete {
			return fault.New(fault.Invalid, "HTTP rate-limit retry classification exceeded traversal bounds")
		}
		if matched && rejected != nil {
			decision, found = rejected.decision, true
		}
		return nil
	})
	if failure != nil {
		return failure
	}
	if found {
		setRateLimitHeaders(header, decision)
	}
	return nil
}

func rateLimitSeconds(duration time.Duration) string {
	seconds := duration / time.Second
	if duration%time.Second != 0 {
		seconds++
	}
	return strconv.FormatInt(int64(seconds), 10)
}
