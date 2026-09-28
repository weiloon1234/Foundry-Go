package ratelimit_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type backendFunc func(context.Context, ratelimit.Key, ratelimit.Limit, uint32) (ratelimit.Decision, error)

func (f backendFunc) RateLimit(ctx context.Context, k ratelimit.Key, l ratelimit.Limit, c uint32) (ratelimit.Decision, error) {
	return f(ctx, k, l, c)
}
func accepted(_ context.Context, _ ratelimit.Key, l ratelimit.Limit, c uint32) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: true, Limit: l.Requests, Remaining: l.Requests - c, ResetAfter: l.Window}, nil
}
func fixture(t *testing.T, b ratelimit.Backend, config func(*ratelimit.Config), codec keyspace.Codec[string]) (ratelimit.Limiter[string], *ratelimit.Store) {
	t.Helper()
	c := ratelimit.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "limits"})
	if config != nil {
		config(&c)
	}
	s, err := ratelimit.NewStore(b, c)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ratelimit.Define("requests", codec, ratelimit.PerSecond(5)).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	return l, s
}
func TestDeclarationIdentityPolicyAndValidation(t *testing.T) {
	_, s := fixture(t, backendFunc(accepted), nil, keyspace.StringKeys[string]())
	if _, err := ratelimit.Define("requests", keyspace.StringKeys[string](), ratelimit.PerSecond(5)).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	d := ratelimit.Define("new", keyspace.StringKeys[string](), ratelimit.PerSecond(4))
	first, err := d.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Bind(s); err != nil {
		t.Fatal(err)
	}
	snapshot := first.Limit()
	snapshot.Requests = 99
	if first.Limit().Requests != 4 {
		t.Fatal("policy mutated")
	}
	for _, limit := range []ratelimit.Limit{{}, {Requests: 1}, {Requests: 1, Window: time.Nanosecond}, {Requests: 1, Window: time.Millisecond + 1}, {Requests: 1, Window: ratelimit.MaxWindow + time.Millisecond}} {
		if err := limit.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal(limit, err)
		}
	}
	var zero ratelimit.Limiter[string]
	if _, err := zero.Allow(t.Context(), "a"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestCallbacksFailuresAndLateReplies(t *testing.T) {
	for _, behavior := range []string{"panic", "goexit", "error", "canceled", "malformed"} {
		t.Run(behavior, func(t *testing.T) {
			sentinel := errors.New("unavailable")
			b := backendFunc(func(ctx context.Context, k ratelimit.Key, l ratelimit.Limit, c uint32) (ratelimit.Decision, error) {
				switch behavior {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "error":
					return ratelimit.Decision{Allowed: true}, sentinel
				case "canceled":
					<-ctx.Done()
					return accepted(ctx, k, l, c)
				case "malformed":
					return ratelimit.Decision{Allowed: true}, nil
				}
				panic("unreachable")
			})
			l, _ := fixture(t, b, func(c *ratelimit.Config) { c.Timeout = 10 * time.Millisecond }, keyspace.StringKeys[string]())
			d, err := l.Allow(t.Context(), "a")
			if err == nil || d != (ratelimit.Decision{}) || strings.Contains(err.Error(), "private") {
				t.Fatal(d, err)
			}
			if behavior == "error" && !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			if behavior == "canceled" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
}
func TestResolverOwnsCapacityUntilExit(t *testing.T) {
	var calls atomic.Int32
	l, _ := fixture(t, backendFunc(func(ctx context.Context, k ratelimit.Key, l ratelimit.Limit, c uint32) (ratelimit.Decision, error) {
		calls.Add(1)
		return accepted(ctx, k, l, c)
	}), func(c *ratelimit.Config) { c.MaxConcurrent = 1 }, keyspace.StringKeys[string]())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := l.TakeWith(ctx, 1, func(context.Context) (string, error) { close(entered); <-release; return "a", nil })
		finished <- err
	}()
	<-entered
	cancel()
	if _, err := l.Allow(t.Context(), "b"); !errors.Is(err, fault.Conflict) {
		t.Error(err)
	}
	select {
	case err := <-finished:
		t.Error("callback abandoned", err)
	default:
	}
	close(release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("canceled resolver reached backend")
	}
	if d, err := l.Allow(t.Context(), "c"); err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
}
func TestResolverAndCodecCannotEscapeBoundary(t *testing.T) {
	for _, where := range []string{"resolver", "codec"} {
		for _, kind := range []string{"panic", "goexit", "oversize"} {
			t.Run(where+"-"+kind, func(t *testing.T) {
				fn := func() (string, error) {
					switch kind {
					case "panic":
						panic("private")
					case "goexit":
						runtime.Goexit()
					}
					return strings.Repeat("a", 1025), nil
				}
				codec := keyspace.StringKeys[string]()
				resolve := func(context.Context) (string, error) { return "key", nil }
				if where == "codec" {
					codec = keyspace.NewCodec(func(string) (string, error) { return fn() })
				} else {
					resolve = func(context.Context) (string, error) { return fn() }
				}
				var calls atomic.Int32
				l, _ := fixture(t, backendFunc(func(ctx context.Context, k ratelimit.Key, l ratelimit.Limit, c uint32) (ratelimit.Decision, error) {
					calls.Add(1)
					return accepted(ctx, k, l, c)
				}), nil, codec)
				d, err := l.TakeWith(t.Context(), 1, resolve)
				if err == nil || d.Allowed || calls.Load() != 0 || strings.Contains(err.Error(), "private") {
					t.Fatal(d, err, calls.Load())
				}
			})
		}
	}
}
func TestMalformedDecisions(t *testing.T) {
	good, _ := accepted(t.Context(), ratelimit.Key{}, ratelimit.PerSecond(5), 1)
	cases := []ratelimit.Decision{{}, good, good, good, good, good, good}
	cases[1].Limit = 6
	cases[2].Remaining = 5
	cases[3].ResetAfter = 0
	cases[4].ResetAfter++
	cases[5].RetryAfter = time.Second
	cases[6].Allowed = false
	cases[6].RetryAfter = time.Second
	for _, d := range cases {
		if err := d.Validate(ratelimit.PerSecond(5), 1); err == nil {
			t.Fatal(d)
		}
	}
}

func TestStoreAndKeyBoundsRejectBeforeBackend(t *testing.T) {
	namespace := keyspace.Namespace{Application: "test", Environment: "bounds"}
	for _, change := range []func(*ratelimit.Config){func(c *ratelimit.Config) { c.Namespace.Application = "" }, func(c *ratelimit.Config) { c.MaxKeyBytes = keyspace.MaxKeyBytes + 1 }, func(c *ratelimit.Config) { c.MaxConcurrent = 0 }, func(c *ratelimit.Config) { c.Timeout = 0 }, func(c *ratelimit.Config) { c.MaxDeclarations = 0 }} {
		config := ratelimit.DefaultConfig(namespace)
		change(&config)
		if _, err := ratelimit.NewStore(backendFunc(accepted), config); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := ratelimit.NewStore(nil, ratelimit.DefaultConfig(namespace)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var calls atomic.Int32
	limiter, store := fixture(t, backendFunc(func(ctx context.Context, k ratelimit.Key, l ratelimit.Limit, c uint32) (ratelimit.Decision, error) {
		calls.Add(1)
		return accepted(ctx, k, l, c)
	}), func(c *ratelimit.Config) { c.MaxDeclarations = 1 }, keyspace.StringKeys[string]())
	if _, err := ratelimit.Define("second", keyspace.StringKeys[string](), ratelimit.PerSecond(1)).Bind(store); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, key := range []string{"", "bad\x00key", string([]byte{255}), strings.Repeat("x", 1025)} {
		if _, err := limiter.Allow(t.Context(), key); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := limiter.TakeWith(t.Context(), 1, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := limiter.Allow(nil, "a"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
}
