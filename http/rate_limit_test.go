package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	stdhttp "net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"strings"
	"testing"
	"time"
)

type rateTestClock struct{}

func (rateTestClock) Now() time.Time { return time.UnixMilli(1250) }
func rateStore(t *testing.T, b ratelimit.Backend) *ratelimit.Store {
	t.Helper()
	if b == nil {
		local, err := memory.New(100, rateTestClock{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { local.Close() })
		b = local
	}
	s, err := ratelimit.NewStore(b, ratelimit.DefaultConfig(keyspace.Namespace{Application: "http", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestRateLimitHTTPHeadersEnvelopeAndNativeHandler(t *testing.T) {
	s := rateStore(t, nil)
	l, err := ratelimit.Define("requests", keyspace.StringKeys[string](), ratelimit.PerSecond(2)).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	var writer stdhttp.ResponseWriter
	calls := 0
	var native *stdhttp.Request
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		calls++
		if w != writer || r != native {
			t.Error("native handler inputs replaced")
		}
		w.WriteHeader(204)
	}), RateLimit(l, func(r *stdhttp.Request) (string, error) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("resolver deadline missing")
		}
		return "member", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{204, 204, 429, 429} {
		w := httptest.NewRecorder()
		writer = w
		native = httptest.NewRequest("GET", "/", nil)
		handler.ServeHTTP(w, native)
		if w.Code != status || w.Header().Get("X-RateLimit-Limit") != "2" || w.Header().Get("X-RateLimit-Reset") != "1" {
			t.Fatal(w.Code, w.Header())
		}
		if status == 429 {
			var body ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != RateLimited || w.Header().Get("Retry-After") != "1" || w.Header().Get("X-RateLimit-Remaining") != "0" {
				t.Fatal(body, w.Header(), err)
			}
		} else if w.Header().Get("Retry-After") != "" {
			t.Fatal(w.Header())
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

type rateErrorBackend struct{ err error }

func (b rateErrorBackend) RateLimit(context.Context, ratelimit.Key, ratelimit.Limit, uint32) (ratelimit.Decision, error) {
	return ratelimit.Decision{}, b.err
}
func TestRateLimitHTTPFailuresAndResolvers(t *testing.T) {
	for _, kind := range []string{"backend", "key-error", "panic", "goexit", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			var b ratelimit.Backend
			if kind == "backend" {
				b = rateErrorBackend{errors.New("private backend failure")}
			}
			l, err := ratelimit.Define("requests", keyspace.StringKeys[string](), ratelimit.PerSecond(1)).Bind(rateStore(t, b))
			if err != nil {
				t.Fatal(err)
			}
			h, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) { t.Error("failed rate limit reached handler") }), RateLimit(l, func(*stdhttp.Request) (string, error) {
				switch kind {
				case "key-error":
					return "", Unauthenticated
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				}
				return "key", nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/", nil)
			if kind == "canceled" {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 503
			if kind == "key-error" {
				want = 401
			}
			if w.Code != want || strings.Contains(w.Body.String(), "private") || w.Header().Get("Retry-After") != "" {
				t.Fatal(w.Code, w.Body.String(), w.Header())
			}
		})
	}
}
func TestRateLimitIPAttributionAndUntrustedHeaders(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(map[bool]string{false: "peer", true: "trusted"}[trusted], func(t *testing.T) {
			l, err := ratelimit.Define("ip", keyspace.TextKeys[netip.Addr](), ratelimit.PerSecond(1)).Bind(rateStore(t, nil))
			if err != nil {
				t.Fatal(err)
			}
			middleware := []Middleware{}
			if trusted {
				middleware = append(middleware, TrustedProxy(TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Headers: []ProxyHeader{XForwardedForHeader()}}))
			}
			middleware = append(middleware, RateLimitByIP(l))
			h, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) }), middleware...)
			if err != nil {
				t.Fatal(err)
			}
			for i, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.1"} {
				r := httptest.NewRequest("GET", "/", nil)
				r.RemoteAddr = "10.0.0.1:8000"
				r.Header.Set("X-Forwarded-For", ip)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := 429
				if i == 0 || (trusted && i == 1) {
					want = 204
				}
				if w.Code != want {
					t.Fatal(i, w.Code, w.Body.String())
				}
			}
		})
	}
}
func TestRateLimitIPNormalizationInvalidPeerAndAssembly(t *testing.T) {
	l, err := ratelimit.Define(ratelimit.Name(strings.Repeat("A", 128)), keyspace.TextKeys[netip.Addr](), ratelimit.PerSecond(1)).Bind(rateStore(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	next := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) })
	h, err := ApplyMiddleware(next, RateLimitByIP(l))
	if err != nil {
		t.Fatal(err)
	}
	for i, peer := range []string{"[::ffff:192.0.2.1]:8000", "192.0.2.1:9000", "invalid"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = peer
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := []int{204, 429, 400}[i]
		if w.Code != want {
			t.Fatal(peer, w.Code)
		}
	}
	if _, err := ApplyMiddleware(next, RateLimitByIP(l), RateLimitByIP(l)); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := ApplyMiddleware(next, RateLimit(l, nil)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := ApplyMiddleware(next, RateLimitByIP(ratelimit.Limiter[netip.Addr]{})); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestRateLimitRouteScopes(t *testing.T) {
	b, err := memory.New(10, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	s := rateStore(t, b)
	registrations := []RouteRegistration{}
	for _, name := range []string{"first", "second"} {
		l, err := ratelimit.Define(ratelimit.Name(name), keyspace.StringKeys[string](), ratelimit.PerHour(1)).Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		registrations = append(registrations, staticRoute(RouteID(name), stdhttp.MethodGet, "/"+name).WithMiddleware(RateLimit(l, func(r *stdhttp.Request) (string, error) {
			if info, ok := MatchedRoute(r.Context()); !ok || info.ID != RouteID(name) {
				t.Error("route metadata missing")
			}
			return "same", nil
		})).HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) }))
	}
	router, err := NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/first", "/second"} {
		for _, want := range []int{204, 429} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != want {
				t.Fatal(path, w.Code)
			}
		}
	}
}
