package cache_test

import (
	"context"
	"errors"
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
		c := boundProfiles(t, func(c *cache.Config) { c.MaxFills = 1; c.MaxFillWaiters = 1 })
		release := make(chan struct{})
		owner := asyncRemember(c, t.Context(), "owner", func(context.Context) (profile, error) {
			<-release
			return profile{Name: "complete"}, nil
		})
		synctest.Wait()
		if _, err := c.Remember(t.Context(), "other", cache.Forever(), noLoad); !errors.Is(err, fault.Conflict) {
			t.Fatal("fill limit", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		follower := asyncRemember(c, ctx, "owner", noLoad)
		synctest.Wait()
		if _, err := c.Remember(t.Context(), "owner", cache.Forever(), noLoad); !errors.Is(err, fault.Conflict) {
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
		if _, err := c.Remember(t.Context(), "other", cache.Forever(), func(context.Context) (profile, error) { return profile{}, nil }); err != nil {
			t.Fatal("slot leaked", err)
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

func TestRememberOwnerCancellationWaitsForLoaderExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := boundProfiles(t, nil)
		ctx, cancel := context.WithCancel(t.Context())
		release := make(chan struct{})
		owner := asyncRemember(c, ctx, "key", func(ctx context.Context) (profile, error) { <-ctx.Done(); <-release; return profile{Name: "late"}, nil })
		synctest.Wait()
		follower := asyncRemember(c, t.Context(), "key", noLoad)
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case result := <-owner:
			t.Fatal("abandoned loader", result)
		default:
		}
		select {
		case result := <-follower:
			t.Fatal("published before loader exit", result)
		default:
		}
		close(release)
		for _, result := range []<-chan rememberResult{owner, follower} {
			if got := <-result; !errors.Is(got.err, context.Canceled) {
				t.Fatal(got)
			}
		}
		if _, found, err := c.Get(t.Context(), "key"); err != nil || found {
			t.Fatal("canceled value cached", found, err)
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
func TestRememberBackendWriteFailuresReleaseTheFlight(t *testing.T) {
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
				s, err := cache.NewStore(wrapped, cache.DefaultConfig(namespace))
				if err != nil {
					t.Fatal(err)
				}
				c, err := profiles.Bind(s)
				if err != nil {
					t.Fatal(err)
				}
				release := make(chan struct{})
				owner := asyncRemember(c, t.Context(), "key", func(context.Context) (profile, error) { <-release; return profile{}, nil })
				synctest.Wait()
				follower := asyncRemember(c, t.Context(), "key", noLoad)
				synctest.Wait()
				close(release)
				for _, result := range []<-chan rememberResult{owner, follower} {
					if got := <-result; !errors.Is(got.err, test.target) || strings.Contains(got.err.Error(), "private") {
						t.Fatal(got)
					}
				}
				if writes.Load() != 1 {
					t.Fatal("implicitly retried write", writes.Load())
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
