package cache_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type rememberResult struct {
	value profile
	err   error
}

func asyncRemember(c cache.Cache[userKey, profile], ctx context.Context, key userKey, loader func(context.Context) (profile, error)) <-chan rememberResult {
	result := make(chan rememberResult, 1)
	go func() {
		value, err := c.Remember(ctx, key, cache.For(time.Minute), loader)
		result <- rememberResult{value, err}
	}()
	return result
}
func noLoad(context.Context) (profile, error) { return profile{}, errors.New("unexpected loader") }
func boundProfiles(t *testing.T, configure func(*cache.Config)) cache.Cache[userKey, profile] {
	t.Helper()
	s, _, _ := store(t, configure)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRememberCoalescesMissesAndOwnsEverySnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := boundProfiles(t, nil)
		release := make(chan struct{})
		var loads atomic.Int32
		input := profile{Name: "snapshot", Scores: []int{1}, Attributes: map[string]any{"key": "value"}}
		owner := asyncRemember(c, t.Context(), "key", func(context.Context) (profile, error) {
			loads.Add(1)
			<-release
			return input, nil
		})
		synctest.Wait()
		followers := make([]<-chan rememberResult, 16)
		for i := range followers {
			followers[i] = asyncRemember(c, t.Context(), "key", noLoad)
		}
		synctest.Wait()
		close(release)
		first := <-owner
		if first.err != nil || first.value.Name != "snapshot" {
			t.Fatal(first)
		}
		first.value.Scores[0] = 9
		first.value.Attributes["key"] = "changed"
		for _, result := range followers {
			got := <-result
			if got.err != nil || got.value.Scores[0] != 1 || got.value.Attributes["key"] != "value" {
				t.Fatal(got)
			}
			got.value.Scores[0] = 8
		}
		input.Scores[0] = 7
		got, err := c.Remember(t.Context(), "key", cache.Forever(), noLoad)
		if err != nil || got.Scores[0] != 1 || loads.Load() != 1 {
			t.Fatal(got, err, loads.Load())
		}
	})
}

func TestRememberBoundsAndCanceledFollowersReleaseTheirSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := store(t, func(c *cache.Config) { c.MaxFills = 1; c.MaxFillWaiters = 1 })
		c, err := profiles.Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		owner := asyncRemember(c, t.Context(), "owner", func(context.Context) (profile, error) {
			<-release
			return profile{Name: "complete"}, nil
		})
		synctest.Wait()
		// A full fill registry loads directly instead of failing the caller.
		direct, err := c.Remember(t.Context(), "other", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "direct"}, nil })
		if err != nil || direct.Name != "direct" {
			t.Fatal("fill limit", direct, err)
		}
		if stats := s.Stats(); stats.Uncoalesced != 1 || stats.Loads != 2 {
			t.Fatal("uncoalesced load not counted", stats)
		}
		ctx, cancel := context.WithCancel(t.Context())
		follower := asyncRemember(c, ctx, "owner", noLoad)
		synctest.Wait()
		if _, err := c.Remember(t.Context(), "owner", cache.Forever(), noLoad); !errors.Is(err, fault.Overloaded) {
			t.Fatal("waiter limit", err)
		}
		cancel()
		if result := <-follower; !errors.Is(result.err, context.Canceled) {
			t.Fatal(result)
		}
		replacement := asyncRemember(c, t.Context(), "owner", noLoad)
		synctest.Wait()
		close(release)
		if result := <-owner; result.err != nil {
			t.Fatal(result)
		}
		if result := <-replacement; result.err != nil || result.value.Name != "complete" {
			t.Fatal(result)
		}
		if _, err := c.Remember(t.Context(), "third", cache.Forever(), func(context.Context) (profile, error) { return profile{}, nil }); err != nil {
			t.Fatal("slot leaked", err)
		}
		if stats := s.Stats(); stats.Uncoalesced != 1 {
			t.Fatal("released slot was not reused", stats)
		}
	})
}

func TestRememberDifferentKeysAndStoresLoadIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first, second := boundProfiles(t, nil), boundProfiles(t, nil)
		release := make(chan struct{})
		blocked := asyncRemember(first, t.Context(), "key", func(context.Context) (profile, error) { <-release; return profile{}, nil })
		synctest.Wait()
		for _, test := range []struct {
			c   cache.Cache[userKey, profile]
			key userKey
		}{{first, "other"}, {second, "key"}} {
			if _, err := test.c.Remember(t.Context(), test.key, cache.Forever(), func(context.Context) (profile, error) { return profile{}, nil }); err != nil {
				t.Fatal(err)
			}
		}
		close(release)
		if result := <-blocked; result.err != nil {
			t.Fatal(result)
		}
	})
}

func TestRememberFailureReachesFollowersAndPermitsFreshAttempt(t *testing.T) {
	cause := errors.New("private loader detail")
	for _, test := range []struct {
		name   string
		loader func(context.Context) (profile, error)
		target error
	}{
		{"error", func(context.Context) (profile, error) { return profile{Name: "partial"}, cause }, cause},
		{"panic", func(context.Context) (profile, error) { panic("private panic detail") }, fault.Panicked},
		{"goexit", func(context.Context) (profile, error) { runtime.Goexit(); return profile{}, nil }, fault.Panicked},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := boundProfiles(t, nil)
				release := make(chan struct{})
				owner := asyncRemember(c, t.Context(), "key", func(ctx context.Context) (profile, error) { <-release; return test.loader(ctx) })
				synctest.Wait()
				follower := asyncRemember(c, t.Context(), "key", noLoad)
				synctest.Wait()
				close(release)
				for _, result := range []<-chan rememberResult{owner, follower} {
					got := <-result
					if !errors.Is(got.err, test.target) || !reflect.DeepEqual(got.value, profile{}) || strings.Contains(got.err.Error(), "private") {
						t.Fatal(got)
					}
				}
				if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
					t.Fatal("failure cached", found, err)
				}
				if _, err := c.Remember(t.Context(), "key", cache.Forever(), func(context.Context) (profile, error) { return profile{}, nil }); err != nil {
					t.Fatal("flight leaked", err)
				}
			})
		})
	}
}

func TestRememberOwnerCancellationDoesNotFailFollowers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := boundProfiles(t, nil)
		ctx, cancel := context.WithCancel(t.Context())
		release := make(chan struct{})
		loaderCanceled := make(chan bool, 1)
		type traced struct{}
		owner := asyncRemember(c, context.WithValue(ctx, traced{}, "request"), "key", func(ctx context.Context) (profile, error) {
			<-release
			if ctx.Value(traced{}) != "request" {
				return profile{}, errors.New("loader lost request values")
			}
			loaderCanceled <- ctx.Err() != nil
			return profile{Name: "late"}, nil
		})
		synctest.Wait()
		follower := asyncRemember(c, t.Context(), "key", noLoad)
		synctest.Wait()
		cancel()
		// The canceled owner stops waiting; its detached loader keeps running.
		if got := <-owner; !errors.Is(got.err, context.Canceled) {
			t.Fatal(got)
		}
		select {
		case result := <-follower:
			t.Fatal("follower failed with its owner", result)
		default:
		}
		close(release)
		if <-loaderCanceled {
			t.Fatal("loader inherited owner cancellation")
		}
		if got := <-follower; got.err != nil || got.value.Name != "late" {
			t.Fatal(got)
		}
		if got, found, err := c.Get(t.Context(), "key"); err != nil || !found || got.Name != "late" {
			t.Fatal("detached fill was not published", got, found, err)
		}
	})
}

func TestRememberFollowersReelectWhenLoaderUsesOwnerContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := boundProfiles(t, nil)
		request, cancel := context.WithCancel(t.Context())
		// This loader ignores its supplied context and waits on the request.
		owner := asyncRemember(c, request, "key", func(context.Context) (profile, error) {
			<-request.Done()
			return profile{}, request.Err()
		})
		synctest.Wait()
		follower := asyncRemember(c, t.Context(), "key", func(context.Context) (profile, error) { return profile{Name: "reelected"}, nil })
		synctest.Wait()
		cancel()
		if got := <-owner; !errors.Is(got.err, context.Canceled) {
			t.Fatal(got)
		}
		if got := <-follower; got.err != nil || got.value.Name != "reelected" {
			t.Fatal("follower inherited the owner's cancellation", got)
		}
	})
}

func TestRememberLoadTimeoutIsIndependentOfOperationTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := boundProfiles(t, func(c *cache.Config) { c.Timeout = time.Second; c.LoadTimeout = 3 * time.Second })
		slow := func(ctx context.Context) (profile, error) {
			select {
			case <-time.After(2 * time.Second):
				return profile{Name: "slow"}, nil
			case <-ctx.Done():
				return profile{}, ctx.Err()
			}
		}
		if got, err := c.Remember(t.Context(), "slow", cache.Forever(), slow); err != nil || got.Name != "slow" {
			t.Fatal("store timeout capped the loader", got, err)
		}
		if _, err := c.Remember(t.Context(), "narrow", cache.Forever(), slow, cache.WithLoadTimeout(time.Second)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("per-call load timeout ignored", err)
		}
		for _, invalid := range []time.Duration{0, -time.Second, cache.MaxLoadTimeout + 1} {
			if _, err := c.Remember(t.Context(), "invalid", cache.Forever(), slow, cache.WithLoadTimeout(invalid)); !errors.Is(err, fault.Invalid) {
				t.Fatal(invalid, err)
			}
		}
	})
}

func TestRememberTimeoutAndRecursiveCycles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := boundProfiles(t, func(c *cache.Config) { c.Timeout = time.Second })
		if _, err := c.Remember(t.Context(), "timeout", cache.Forever(), func(ctx context.Context) (profile, error) { <-ctx.Done(); return profile{}, ctx.Err() }); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		for _, nested := range []bool{false, true} {
			_, err := c.Remember(t.Context(), "a", cache.Forever(), func(ctx context.Context) (profile, error) {
				child := func(ctx context.Context) (profile, error) { return c.Remember(ctx, "a", cache.Forever(), noLoad) }
				if nested {
					return c.Remember(ctx, "b", cache.Forever(), child)
				}
				return child(ctx)
			})
			if !errors.Is(err, fault.Cycle) {
				t.Fatal("cycle not rejected", nested, err)
			}
		}
		if _, err := c.Remember(t.Context(), "a", cache.Forever(), func(context.Context) (profile, error) { return profile{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
}

// A controlled adapter models an external writer between initial miss and election.
type recheckBackend struct {
	cache.TaggedBackend
	cache.Backend
	reads int
}

func (b *recheckBackend) GetTagged(ctx context.Context, key cache.TaggedKey) ([]byte, bool, error) {
	b.reads++
	if b.reads == 1 {
		return nil, false, nil
	}
	return b.TaggedBackend.GetTagged(ctx, key)
}
func TestRememberRechecksStorageAndValidatesBeforeLoading(t *testing.T) {
	s, backend, _ := store(t, nil)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "key", profile{Name: "already filled"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	wrapper := &recheckBackend{Backend: backend, TaggedBackend: backend}
	s, err = cache.NewStore(wrapper, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err = profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Remember(t.Context(), "key", cache.Forever(), noLoad); err != nil || got.Name != "already filled" || wrapper.reads != 2 {
		t.Fatal(got, err, wrapper.reads)
	}
	if stats := s.Stats(); stats.Loads != 0 || stats.Writes != 0 {
		t.Fatal("a successful recheck counted a loader or publication that never ran", stats)
	}
	if _, err := c.Remember(t.Context(), "key", cache.TTL{}, noLoad); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.Remember(t.Context(), "key", cache.Forever(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.Remember(nil, "key", cache.Forever(), noLoad); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var zero cache.Cache[userKey, profile]
	if _, err := zero.Remember(t.Context(), "key", cache.Forever(), noLoad); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestRememberTTLAndConcurrentForgetBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, clock := store(t, nil)
		c, err := profiles.Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		owner := asyncRemember(c, t.Context(), "key", func(context.Context) (profile, error) { <-release; return profile{Name: "fill"}, nil })
		synctest.Wait()
		if _, err := c.Forget(t.Context(), "key"); err != nil {
			t.Fatal(err)
		}
		close(release)
		if got := <-owner; got.err != nil {
			t.Fatal(got)
		}
		if _, found, err := c.Get(t.Context(), "key"); err != nil || !found {
			t.Fatal("fill must remain independent of Forget", found, err)
		}
		clock.Advance(time.Minute)
		if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
			t.Fatal("fill TTL ignored", found, err)
		}
	})
}

// A failing adapter verifies the fill is released even when writing exits abnormally.
type writeBackend struct {
	cache.Backend
	write func(context.Context, cache.EntryKey, []byte, cache.TTL) error
}

func (b writeBackend) Put(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) error {
	return b.write(ctx, key, data, ttl)
}
func TestRememberPublicationFailureReturnsLoadedValue(t *testing.T) {
	cause := errors.New("private write detail")
	for _, test := range []struct {
		name   string
		fail   func() error
		target error
	}{
		{"error", func() error { return cause }, cause},
		{"panic", func() error { panic("private panic detail") }, fault.Panicked},
		{"goexit", func() error { runtime.Goexit(); return nil }, fault.Panicked},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				_, backend, _ := store(t, nil)
				var writes atomic.Int32
				wrapped := writeBackend{Backend: backend, write: func(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) error {
					if writes.Add(1) == 1 {
						return test.fail()
					}
					return backend.Put(ctx, key, data, ttl)
				}}
				var logs bytes.Buffer
				s, err := cache.NewStore(wrapped, cache.DefaultConfig(namespace), cache.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
				if err != nil {
					t.Fatal(err)
				}
				c, err := profiles.Bind(s)
				if err != nil {
					t.Fatal(err)
				}
				release := make(chan struct{})
				owner := asyncRemember(c, t.Context(), "key", func(context.Context) (profile, error) { <-release; return profile{Name: "loaded"}, nil })
				synctest.Wait()
				follower := asyncRemember(c, t.Context(), "key", noLoad)
				synctest.Wait()
				close(release)
				// A cache write failure never fails callers that have a correct value.
				for _, result := range []<-chan rememberResult{owner, follower} {
					if got := <-result; got.err != nil || got.value.Name != "loaded" {
						t.Fatal(got)
					}
				}
				if writes.Load() != 1 {
					t.Fatal("implicitly retried write", writes.Load())
				}
				if stats := s.Stats(); stats.WriteFailures != 1 || stats.Writes != 0 {
					t.Fatal("publication failure not recorded", stats)
				}
				record := logs.String()
				if !strings.Contains(record, "cache fill publication failed") || !strings.Contains(record, `"cache":"profiles"`) || strings.Contains(record, "private") {
					t.Fatal("unsafe or missing failure record", record)
				}
				if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
					t.Fatal("failed publication became visible", found, err)
				}
				if _, err := c.Remember(t.Context(), "key", cache.Forever(), func(context.Context) (profile, error) { return profile{}, nil }); err != nil {
					t.Fatal("write flight leaked", err)
				}
			})
		})
	}
}

func TestRememberCodecFailureAndSizeLimitReleaseTheFlight(t *testing.T) {
	cause := errors.New("private encode detail")
	for _, test := range []struct {
		name   string
		encode func(profile) ([]byte, error)
		target error
	}{
		{"error", func(profile) ([]byte, error) { return nil, cause }, cause},
		{"panic", func(profile) ([]byte, error) { panic("private encode detail") }, fault.Panicked},
		{"goexit", func(profile) ([]byte, error) { runtime.Goexit(); return nil, nil }, fault.Panicked},
		{"size", func(profile) ([]byte, error) { return make([]byte, 32), nil }, fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _, _ := store(t, func(c *cache.Config) { c.MaxValueBytes = 16; c.MaxFills = 1 })
			var encodes int
			codec := cache.NewCodec(func(p profile) ([]byte, error) {
				encodes++
				if encodes == 1 {
					return test.encode(p)
				}
				return []byte("valid"), nil
			}, func(data []byte) (profile, error) { return profile{Name: string(data)}, nil })
			c, err := cache.Define("encode", cache.StringKeys[userKey](), codec).Bind(s)
			if err != nil {
				t.Fatal(err)
			}
			load := func(context.Context) (profile, error) { return profile{}, nil }
			if _, err := c.Remember(t.Context(), "key", cache.Forever(), load); !errors.Is(err, test.target) || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if got, err := c.Remember(t.Context(), "key", cache.Forever(), load); err != nil || got.Name != "valid" {
				t.Fatal("codec flight leaked", got, err)
			}
		})
	}
}

func TestRememberCanceledBackendMissDoesNotInvokeLoader(t *testing.T) {
	_, backend, _ := store(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	wrapped := readBackend{Backend: backend, read: func(context.Context, cache.EntryKey) ([]byte, bool, error) { cancel(); return nil, false, nil }}
	s, err := cache.NewStore(wrapped, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Remember(ctx, "key", cache.Forever(), noLoad); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
