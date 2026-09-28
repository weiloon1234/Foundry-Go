package lease_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
)

var family = lease.Define("documents", keyspace.StringKeys[string]())

func manager(t *testing.T, backend lease.Backend, configure func(*lease.Config)) (*lease.Manager, lease.Leases[string]) {
	t.Helper()
	if backend == nil {
		b, _ := memory.New(32)
		backend = b
		t.Cleanup(func() { b.Close() })
	}
	config := lease.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "leases"})
	if configure != nil {
		configure(&config)
	}
	m, err := lease.NewManager(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	l, err := family.Bind(m)
	if err != nil {
		t.Fatal(err)
	}
	return m, l
}
func acquire(t *testing.T, l lease.Leases[string], key string) *lease.Guard {
	t.Helper()
	g, ok, err := l.TryAcquire(t.Context(), key, time.Second)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	t.Cleanup(func() { g.Release(context.Background()) })
	return g
}

type wrapped struct {
	lease.Backend
	acquire func(context.Context, lease.Key, lease.Owner, time.Duration) (bool, error)
	renew   func(context.Context, lease.Key, lease.Owner, time.Duration) (bool, error)
	release func(context.Context, lease.Key, lease.Owner) (bool, error)
}

func (b wrapped) LeaseAcquire(c context.Context, k lease.Key, o lease.Owner, d time.Duration) (bool, error) {
	if b.acquire != nil {
		return b.acquire(c, k, o, d)
	}
	return b.Backend.LeaseAcquire(c, k, o, d)
}
func (b wrapped) LeaseRenew(c context.Context, k lease.Key, o lease.Owner, d time.Duration) (bool, error) {
	if b.renew != nil {
		return b.renew(c, k, o, d)
	}
	return b.Backend.LeaseRenew(c, k, o, d)
}
func (b wrapped) LeaseRelease(c context.Context, k lease.Key, o lease.Owner) (bool, error) {
	if b.release != nil {
		return b.release(c, k, o)
	}
	return b.Backend.LeaseRelease(c, k, o)
}
func TestAcquireWaitAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, l := manager(t, nil, nil)
		g := acquire(t, l, "a")
		if _, ok, err := l.TryAcquire(t.Context(), "a", time.Second); err != nil || ok {
			t.Fatal(ok, err)
		}
		if _, ok, err := l.Acquire(t.Context(), "a", time.Second, 20*time.Millisecond); ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(ok, err)
		}
		g.Release(t.Context())
		g, ok, err := l.Acquire(t.Context(), "a", time.Second, 20*time.Millisecond)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		time.Sleep(50 * time.Millisecond)
		if err := g.Err(); err != nil {
			t.Fatal("wait deadline canceled ownership", err)
		}
		time.Sleep(time.Second)
		<-g.Done()
		if !errors.Is(g.Err(), lease.ErrLost) {
			t.Fatal(g.Err())
		}
		if err := g.Renew(t.Context()); !errors.Is(err, lease.ErrLost) {
			t.Fatal(err)
		}
		next := acquire(t, l, "a")
		next.Release(t.Context())
		if m.Stats().Active != 0 {
			t.Fatal(m.Stats())
		}
	})
}
func TestWithHeartbeatAndLoss(t *testing.T) {
	t.Run("renews-across-ttl", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			_, l := manager(t, nil, nil)
			ran, err := l.With(t.Context(), "a", 300*time.Millisecond, 0, func(ctx context.Context) error {
				time.Sleep(900 * time.Millisecond)
				if _, ok, err := l.TryAcquire(ctx, "a", time.Second); ok || err != nil {
					t.Fatalf("heartbeat lost: %v %v", ok, err)
				}
				return ctx.Err()
			})
			if !ran || err != nil {
				t.Fatal(ran, err)
			}
		})
	})
	t.Run("loss-cancels-callback", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, _ := memory.New(2)
			_, l := manager(t, wrapped{Backend: b, renew: func(context.Context, lease.Key, lease.Owner, time.Duration) (bool, error) { return false, nil }}, nil)
			ran, err := l.With(t.Context(), "a", 300*time.Millisecond, 0, func(ctx context.Context) error { <-ctx.Done(); return nil })
			if !ran || !errors.Is(err, lease.ErrLost) {
				t.Fatal(ran, err)
			}
		})
	})
}
func TestLateResponsesNeverRestoreOwnership(t *testing.T) {
	for _, operation := range []string{"acquire", "renew"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b, _ := memory.New(4)
				w := wrapped{Backend: b}
				var calls atomic.Int32
				slow := func(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
					calls.Add(1)
					var ok bool
					var err error
					if operation == "acquire" {
						ok, err = b.LeaseAcquire(ctx, key, owner, ttl)
					} else {
						ok, err = b.LeaseRenew(ctx, key, owner, ttl)
					}
					time.Sleep(2 * ttl)
					return ok, err
				}
				if operation == "acquire" {
					w.acquire = slow
				} else {
					w.renew = slow
				}
				m, l := manager(t, w, nil)
				g, ok, err := l.TryAcquire(t.Context(), "a", 100*time.Millisecond)
				if operation == "acquire" {
					if ok || g != nil || !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal(ok, err)
					}
				} else {
					if err != nil || !ok {
						t.Fatal(ok, err)
					}
					if err := g.Renew(t.Context()); !errors.Is(err, lease.ErrLost) {
						t.Fatal(err)
					}
					<-g.Done()
					if !errors.Is(g.Err(), lease.ErrLost) {
						t.Fatal(g.Err())
					}
				}
				if calls.Load() != 1 || m.Stats().Active != 0 {
					t.Fatal(calls.Load(), m.Stats())
				}
			})
		})
	}
}
func TestCallbacksAndCodecsAreOwned(t *testing.T) {
	for _, mode := range []string{"panic", "goexit", "error"} {
		t.Run(mode, func(t *testing.T) {
			m, l := manager(t, nil, nil)
			sentinel := errors.New("domain failure")
			fn := func(context.Context) error {
				switch mode {
				case "panic":
					panic("private payload")
				case "goexit":
					runtime.Goexit()
				}
				return sentinel
			}
			ran, err := l.With(t.Context(), "a", time.Second, 0, fn)
			want := error(fault.Panicked)
			if mode == "error" {
				want = sentinel
			}
			if !ran || !errors.Is(err, want) || m.Stats().Active != 0 {
				t.Fatal(ran, err, m.Stats())
			}
			next := acquire(t, l, "a")
			next.Release(t.Context())
		})
	}
	for _, mode := range []string{"panic", "goexit"} {
		t.Run("codec-"+mode, func(t *testing.T) {
			m, _ := manager(t, nil, nil)
			bad, err := lease.Define("bad", keyspace.NewCodec(func(string) (string, error) {
				if mode == "panic" {
					panic("private")
				}
				runtime.Goexit()
				return "", nil
			})).Bind(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := bad.TryAcquire(t.Context(), "a", time.Second); ok || !errors.Is(err, fault.Panicked) {
				t.Fatal(ok, err)
			}
			if m.Stats().Active != 0 {
				t.Fatal(m.Stats())
			}
		})
	}
}
func TestCloseRetainsNoncooperativeCallbackAndCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, l := manager(t, nil, func(c *lease.Config) { c.MaxActive = 1 })
		started := make(chan struct{})
		finish := make(chan struct{})
		result := make(chan error, 1)
		go func() {
			_, err := l.With(t.Context(), "a", time.Second, 0, func(ctx context.Context) error { close(started); <-ctx.Done(); <-finish; return nil })
			result <- err
		}()
		<-started
		if _, ok, err := l.TryAcquire(t.Context(), "b", time.Second); ok || !errors.Is(err, fault.Conflict) {
			t.Fatal(ok, err)
		}
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if err := m.Close(canceled); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-m.Done():
			t.Fatal("abandoned callback")
		default:
		}
		if m.Stats().Active != 1 {
			t.Fatal(m.Stats())
		}
		if _, ok, err := l.TryAcquire(t.Context(), "b", time.Second); ok || !errors.Is(err, fault.Closed) {
			t.Fatal(ok, err)
		}
		close(finish)
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := m.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}
func TestConcurrentReleaseAndCloseShareCleanup(t *testing.T) {
	b, _ := memory.New(2)
	var releases atomic.Int32
	m, l := manager(t, wrapped{Backend: b, release: func(ctx context.Context, key lease.Key, owner lease.Owner) (bool, error) {
		releases.Add(1)
		return b.LeaseRelease(ctx, key, owner)
	}}, nil)
	g := acquire(t, l, "a")
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := g.Release(t.Context()); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if err := m.Close(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if releases.Load() != 1 {
		t.Fatal(releases.Load())
	}
}
func TestDeclarationAndInputBoundaries(t *testing.T) {
	m, l := manager(t, nil, func(c *lease.Config) { c.MaxDeclarations = 1; c.MaxKeyBytes = 4 })
	if _, err := family.Bind(m); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Define("documents", keyspace.StringKeys[string]()).Bind(m); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := lease.Define("other", keyspace.StringKeys[string]()).Bind(m); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		key       string
		ttl, wait time.Duration
	}{{"", time.Second, 0}, {"12345", time.Second, 0}, {"a", 0, 0}, {"a", time.Second, -1}, {"a", time.Second, time.Hour}} {
		if _, ok, err := l.Acquire(t.Context(), tt.key, tt.ttl, tt.wait); ok || !errors.Is(err, fault.Invalid) {
			t.Fatal(ok, err)
		}
	}
	if _, ok, err := l.TryAcquire(nil, "a", time.Second); ok || !errors.Is(err, fault.Invalid) {
		t.Fatal(ok, err)
	}
	if ran, err := l.With(t.Context(), "a", time.Second, 0, nil); ran || !errors.Is(err, fault.Invalid) {
		t.Fatal(ran, err)
	}
	if m.Stats().Active != 0 {
		t.Fatal(m.Stats())
	}
}

func TestRenewalAndCleanupUncertainty(t *testing.T) {
	t.Run("renewal-error", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b, _ := memory.New(4)
			uncertain := errors.New("acknowledgement lost")
			_, l := manager(t, wrapped{Backend: b, renew: func(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
				b.LeaseRenew(ctx, key, owner, ttl)
				return false, uncertain
			}}, nil)
			g := acquire(t, l, "a")
			if err := g.Renew(t.Context()); !errors.Is(err, lease.ErrLost) || !errors.Is(err, uncertain) {
				t.Fatal(err)
			}
			<-g.Done()
			if g.Context().Err() == nil {
				t.Fatal("uncertain renewal left live work")
			}
		})
	})
	t.Run("cleanup-error-visible-and-not-retried", func(t *testing.T) {
		b, _ := memory.New(4)
		uncertain := errors.New("cleanup acknowledgement lost")
		var calls atomic.Int32
		m, l := manager(t, wrapped{Backend: b, release: func(ctx context.Context, key lease.Key, owner lease.Owner) (bool, error) {
			calls.Add(1)
			b.LeaseRelease(ctx, key, owner)
			return false, uncertain
		}}, nil)
		g := acquire(t, l, "a")
		for range 2 {
			if err := g.Release(t.Context()); !errors.Is(err, uncertain) {
				t.Fatal(err)
			}
		}
		if err := m.Close(t.Context()); !errors.Is(err, uncertain) {
			t.Fatal(err)
		}
		if calls.Load() != 1 {
			t.Fatal(calls.Load())
		}
	})
	t.Run("waiting-cancellation-leaves-other-owner", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			_, l := manager(t, nil, nil)
			g := acquire(t, l, "a")
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { _, _, err := l.Acquire(ctx, "a", time.Second, time.Second); done <- err }()
			synctest.Wait()
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if err := g.Renew(t.Context()); err != nil {
				t.Fatal("waiter changed owner", err)
			}
		})
	})
}
