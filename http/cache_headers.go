package http

import (
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CacheControlMiddlewareID identifies the built-in response cache policy.
const CacheControlMiddlewareID MiddlewareID = "foundry.cache-control"

// CacheVisibility selects which caches may store a response.
type CacheVisibility uint8

const (
	// CachePrivate permits only the requesting user's browser cache.
	CachePrivate CacheVisibility = iota + 1
	// CachePublic also permits shared caches such as CDNs and proxies.
	CachePublic
)

// maxCacheLifetime bounds every declared cache duration.
const maxCacheLifetime = 365 * 24 * time.Hour

// CachePolicy is a typed Cache-Control declaration for GET/HEAD responses. All
// durations are whole seconds from zero through one year; zero MaxAge sends
// max-age=0. SharedMaxAge (s-maxage) requires CachePublic. NoStore forbids
// storage and excludes every other directive. NoCache stores but revalidates
// each use, which composes with ETags for cheap 304 revalidation. ExpiresFrom,
// when set, also sends an Expires date of now+MaxAge for HTTP/1.0 caches using
// that application clock; HTTP/1.1 caches prefer max-age.
type CachePolicy struct {
	Visibility           CacheVisibility
	MaxAge               time.Duration
	SharedMaxAge         value.Optional[time.Duration]
	StaleWhileRevalidate time.Duration
	StaleIfError         time.Duration
	NoCache              bool
	MustRevalidate       bool
	Immutable            bool
	NoStore              bool
	ExpiresFrom          clock.Clock
}

// NoStoreCachePolicy forbids any cache from storing the response.
func NoStoreCachePolicy() CachePolicy { return CachePolicy{NoStore: true} }

func (p CachePolicy) Validate() error { _, err := p.compile(); return err }

type compiledCachePolicy struct {
	value, private string
	maxAge         time.Duration
	expires        clock.Clock
}

func (p CachePolicy) compile() (compiledCachePolicy, error) {
	invalid := func(message string) (compiledCachePolicy, error) {
		return compiledCachePolicy{}, fault.New(fault.Invalid, message)
	}
	shared, hasShared := p.SharedMaxAge.Get()
	if p.NoStore {
		if p.Visibility != 0 || p.MaxAge != 0 || hasShared || p.StaleWhileRevalidate != 0 || p.StaleIfError != 0 || p.NoCache || p.MustRevalidate || p.Immutable || p.ExpiresFrom != nil {
			return invalid("no-store cache policy cannot declare other directives")
		}
		return compiledCachePolicy{value: "no-store", private: "no-store"}, nil
	}
	if p.Visibility != CachePrivate && p.Visibility != CachePublic {
		return invalid("cache policy requires private or public visibility")
	}
	for _, duration := range []time.Duration{p.MaxAge, shared, p.StaleWhileRevalidate, p.StaleIfError} {
		if duration < 0 || duration > maxCacheLifetime || duration%time.Second != 0 {
			return invalid("cache durations must be whole seconds from zero through one year")
		}
	}
	if hasShared && p.Visibility != CachePublic {
		return invalid("s-maxage requires public cache visibility")
	}
	if p.Immutable && (p.MaxAge == 0 || p.NoCache) {
		return invalid("immutable responses require a positive max-age without no-cache")
	}
	if p.ExpiresFrom != nil && nilCookieValue(p.ExpiresFrom) {
		return invalid("cache Expires requires a non-nil application clock")
	}
	render := func(visibility string, sharedAllowed bool) string {
		directives := []string{visibility, "max-age=" + cacheSeconds(p.MaxAge)}
		if hasShared && sharedAllowed {
			directives = append(directives, "s-maxage="+cacheSeconds(shared))
		}
		if p.StaleWhileRevalidate > 0 {
			directives = append(directives, "stale-while-revalidate="+cacheSeconds(p.StaleWhileRevalidate))
		}
		if p.StaleIfError > 0 {
			directives = append(directives, "stale-if-error="+cacheSeconds(p.StaleIfError))
		}
		if p.NoCache {
			directives = append(directives, "no-cache")
		}
		if p.MustRevalidate {
			directives = append(directives, "must-revalidate")
		}
		if p.Immutable {
			directives = append(directives, "immutable")
		}
		return strings.Join(directives, ", ")
	}
	compiled := compiledCachePolicy{value: render("private", false), maxAge: p.MaxAge, expires: p.ExpiresFrom}
	compiled.private = compiled.value
	if p.Visibility == CachePublic {
		compiled.value = render("public", true)
	}
	return compiled, nil
}

func cacheSeconds(duration time.Duration) string {
	return strconv.FormatInt(int64(duration/time.Second), 10)
}

// CacheControl applies a snapshotted policy to GET and HEAD responses before the
// next handler runs. It never replaces a Cache-Control value an outer
// middleware already chose (for example a browser session's no-store), handlers
// may still override it, and shared error responses replace it with no-store.
// A public policy is downgraded to private for requests carrying an
// Authorization header, so shared caches never store credentialed responses.
// Other methods are unchanged. Compose with ETags for conditional revalidation.
func CacheControl(policy CachePolicy) Middleware {
	compiled, err := policy.compile()
	return defineReplayMiddleware(CacheControlMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			header := w.Header()
			if (r.Method == stdhttp.MethodGet || r.Method == stdhttp.MethodHead) && len(header.Values("Cache-Control")) == 0 {
				directives := compiled.value
				if len(r.Header.Values("Authorization")) != 0 {
					directives = compiled.private
				}
				header.Set("Cache-Control", directives)
				if compiled.expires != nil {
					if now, err := signingTime(compiled.expires, "cache expiry clock"); err == nil {
						header.Set("Expires", now.Add(compiled.maxAge).Format(stdhttp.TimeFormat))
					}
				}
			}
			next.ServeHTTP(w, r)
		}), nil
	})
}
