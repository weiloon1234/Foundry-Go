package cachetest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/internal/cacheatomic"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type AtomicExpiryBackend interface {
	BasicEntryBackend
	Access(context.Context, cache.EntryKey, cacheatomic.Mode, cacheatomic.Change) error
}

// RunContendedExpiry holds the actual adapter lock while a competing metadata
// operation starts. The application clock advances without wall-clock sleeps.
// Reads take no mutation lock: Exists completes while the lock is held and
// evaluates liveness at its own observation time.
func RunContendedExpiry(t *testing.T, prepare func(*testing.T) (AtomicExpiryBackend, *testkit.Clock, cache.EntryKey)) {
	for _, operation := range []string{"exists", "expire", "forever", "relative-ttl"} {
		t.Run(operation, func(t *testing.T) {
			b, clock, key := prepare(t)
			initial := time.Second
			if operation == "relative-ttl" {
				initial = time.Hour
			}
			if err := b.Put(t.Context(), key, []byte("original"), cache.For(initial)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			locked, release := make(chan struct{}), make(chan struct{})
			var unlock sync.Once
			defer unlock.Do(func() { close(release) })
			blocker := make(chan error, 1)
			go func() {
				blocker <- b.Access(ctx, key, cacheatomic.Payload, func(time.Time, *cacheatomic.Record) (*cacheatomic.Record, bool, error) {
					close(locked)
					select {
					case <-release:
					case <-ctx.Done():
					}
					return nil, false, ctx.Err()
				})
			}()
			select {
			case <-locked:
			case <-ctx.Done():
				t.Fatal("lock not acquired")
			}
			if operation == "exists" {
				if found, err := b.Exists(ctx, key); err != nil || !found {
					t.Fatalf("lock-free read blocked or missed a live entry: %v %v", found, err)
				}
				clock.Advance(2 * time.Second)
				if found, err := b.Exists(ctx, key); err != nil || found {
					t.Fatalf("read ignored current time: %v %v", found, err)
				}
				unlock.Do(func() { close(release) })
				if err := <-blocker; err != nil {
					t.Fatal(err)
				}
				return
			}
			waiting := &expiryWaitContext{Context: ctx, entered: make(chan struct{})}
			type outcome struct {
				found bool
				err   error
			}
			finished := make(chan outcome, 1)
			go func() {
				var found bool
				var err error
				switch operation {
				case "forever":
					found, err = b.Expire(waiting, key, cache.Forever())
				default:
					found, err = b.Expire(waiting, key, cache.For(time.Second))
				}
				finished <- outcome{found, err}
			}()
			select {
			case <-waiting.entered:
			case <-ctx.Done():
				t.Fatal("metadata operation not entered")
			}
			clock.Advance(2 * time.Second)
			unlock.Do(func() { close(release) })
			if err := <-blocker; err != nil {
				t.Fatal(err)
			}
			var got outcome
			select {
			case got = <-finished:
			case <-ctx.Done():
				t.Fatal("metadata operation did not finish")
			}
			want := operation == "relative-ttl"
			if got.err != nil || got.found != want {
				t.Fatalf("found=%v want=%v error=%v", got.found, want, got.err)
			}
			clock.Advance(500 * time.Millisecond)
			data, found, err := b.Get(ctx, key)
			if err != nil || found != want || found && string(data) != "original" {
				t.Fatalf("post-contention value: found=%v error=%v", found, err)
			}
			if want {
				clock.Advance(500 * time.Millisecond)
				if found, err := b.Exists(ctx, key); err != nil || found {
					t.Fatalf("renewal did not expire: %v %v", found, err)
				}
			}
		})
	}
}

type expiryWaitContext struct {
	context.Context
	calls   atomic.Int32
	entered chan struct{}
}

func (c *expiryWaitContext) Err() error {
	// The public backend checks once before dispatch; the driver checks again
	// before acquiring its lock. This announces entry without modifying time.
	if c.calls.Add(1) == 2 {
		close(c.entered)
	}
	return c.Context.Err()
}
