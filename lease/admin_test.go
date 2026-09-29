package lease_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
)

func TestForceReleaseBreaksAnyOwner(t *testing.T) {
	_, l := manager(t, nil, nil)
	g := acquire(t, l, "stuck")
	removed, err := l.ForceRelease(t.Context(), "stuck")
	if err != nil || !removed {
		t.Fatal(removed, err)
	}
	if _, ok, err := l.TryAcquire(t.Context(), "stuck", time.Second); err != nil || !ok {
		t.Fatal("forced release did not free the key", ok, err)
	}
	if err := g.Renew(t.Context()); !errors.Is(err, lease.ErrLost) {
		t.Fatal("previous owner kept ownership", err)
	}
	if removed, err := l.ForceRelease(t.Context(), "absent"); err != nil || removed {
		t.Fatal(removed, err)
	}
	type basic struct{ lease.Backend }
	raw, _ := memory.New(4)
	t.Cleanup(func() { raw.Close() })
	_, unsupported := manager(t, basic{raw}, nil)
	if _, err := unsupported.ForceRelease(t.Context(), "x"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestExportedOwnershipRestoresInAnotherManager(t *testing.T) {
	raw, _ := memory.New(8)
	t.Cleanup(func() { raw.Close() })
	_, first := manager(t, raw, nil)
	_, second := manager(t, raw, nil)
	g, ok, err := first.TryAcquire(t.Context(), "job", time.Minute)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	token, err := first.Export(g)
	if err != nil {
		t.Fatal(err)
	}
	<-g.Done()
	if !errors.Is(g.Err(), lease.ErrExported) {
		t.Fatal("exporter kept managing ownership", g.Err())
	}
	if _, ok, err := second.TryAcquire(t.Context(), "job", time.Minute); err != nil || ok {
		t.Fatal("export released the authority key", ok, err)
	}
	text, err := token.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if rendered := fmt.Sprintf(format, token); strings.Contains(rendered, string(text)) || rendered != "[lease token]" {
			t.Fatal("token secret leaked through formatting", rendered)
		}
	}
	var decoded lease.Token[string]
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	restored, err := second.Restore(t.Context(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	// Tokens are single-use: a redelivered token cannot create a second holder.
	if _, err := first.Restore(t.Context(), decoded); !errors.Is(err, lease.ErrLost) {
		t.Fatal("token restored twice", err)
	}
	if err := restored.Renew(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := restored.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Restore(t.Context(), decoded); !errors.Is(err, lease.ErrLost) {
		t.Fatal("released token restored", err)
	}
	other := lease.Define("other-family", keyspace.StringKeys[string]())
	for _, bad := range []string{"", "foundry-lease-v1.", "foundry-lease-v1.!!", string(text) + "x", "prefix" + string(text)} {
		var invalid lease.Token[string]
		if err := invalid.UnmarshalText([]byte(bad)); !errors.Is(err, fault.Invalid) {
			t.Fatal(bad, err)
		}
	}
	mgr, _ := manager(t, raw, nil)
	bound, err := other.Bind(mgr)
	if err != nil {
		t.Fatal(err)
	}
	var foreign lease.Token[string]
	_ = foreign.UnmarshalText(text)
	if _, err := bound.Restore(t.Context(), foreign); !errors.Is(err, fault.Invalid) {
		t.Fatal("token restored into another family", err)
	}
	type basic struct{ lease.Backend }
	_, unsupported := manager(t, basic{raw}, nil)
	if _, err := unsupported.Restore(t.Context(), decoded); !errors.Is(err, fault.Invalid) {
		t.Fatal("restore without owner transfer was accepted", err)
	}
}

func TestSemaphoreBoundsConcurrentHolders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, _ := manager(t, nil, nil)
		semaphore, err := lease.DefineSemaphore("exports", keyspace.StringKeys[string](), 2).Bind(m)
		if err != nil {
			t.Fatal(err)
		}
		if semaphore.Slots() != 2 {
			t.Fatal(semaphore.Slots())
		}
		first, ok, err := semaphore.Acquire(t.Context(), "tenant", time.Minute, 0)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		second, ok, err := semaphore.Acquire(t.Context(), "tenant", time.Minute, 0)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if _, ok, err := semaphore.Acquire(t.Context(), "tenant", time.Minute, 0); err != nil || ok {
			t.Fatal("third holder admitted", ok, err)
		}
		if _, ok, err := semaphore.Acquire(t.Context(), "other-tenant", time.Minute, 0); err != nil || !ok {
			t.Fatal("independent resources share slots", ok, err)
		}
		if _, ok, err := semaphore.Acquire(t.Context(), "tenant", time.Minute, time.Second); ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(ok, err)
		}
		var running atomic.Int32
		done := make(chan error, 1)
		go func() {
			ran, err := semaphore.With(t.Context(), "tenant", time.Minute, time.Minute, func(context.Context) error {
				running.Add(1)
				return nil
			})
			if !ran && err == nil {
				err = errors.New("callback did not run")
			}
			done <- err
		}()
		synctest.Wait()
		if running.Load() != 0 {
			t.Fatal("With exceeded the slot bound")
		}
		if err := first.Release(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil || running.Load() != 1 {
			t.Fatal(err, running.Load())
		}
		if err := second.Release(t.Context()); err != nil {
			t.Fatal(err)
		}
		for _, slots := range []int{0, lease.MaxSemaphoreSlots + 1} {
			if _, err := lease.DefineSemaphore("invalid", keyspace.StringKeys[string](), slots).Bind(m); !errors.Is(err, fault.Invalid) {
				t.Fatal(slots, err)
			}
		}
	})
}

// goexitOnce is an application backend embedding a framework backend; its own
// acquisition calls runtime.Goexit once.
type goexitOnce struct {
	*memory.Backend
	fired atomic.Bool
}

func (b *goexitOnce) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	if !b.fired.Swap(true) {
		runtime.Goexit()
	}
	return b.Backend.LeaseAcquire(ctx, key, owner, ttl)
}

// An embedding application backend stays isolated even though it inherits the
// framework marker, and a contained failure releases its admission slot.
func TestEmbeddedFrameworkBackendStaysIsolated(t *testing.T) {
	raw, _ := memory.New(4)
	t.Cleanup(func() { raw.Close() })
	backend := &goexitOnce{Backend: raw}
	_, l := manager(t, backend, func(c *lease.Config) { c.MaxActive = 1 })
	done := make(chan error, 1)
	go func() {
		_, _, err := l.TryAcquire(t.Context(), "key", time.Second)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, fault.Panicked) {
			t.Fatal("Goexit was not contained", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the caller goroutine exited")
	}
	g, ok, err := l.TryAcquire(t.Context(), "key", time.Second)
	if err != nil || !ok {
		t.Fatal("admission slot leaked", ok, err)
	}
	if err := g.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// countingAcquires counts acquisition commands reaching the authority.
type countingAcquires struct {
	*memory.Backend
	calls atomic.Int32
}

func (b *countingAcquires) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	b.calls.Add(1)
	return b.Backend.LeaseAcquire(ctx, key, owner, ttl)
}

// A waiting acquisition of a full semaphore tries every slot once, then only a
// bounded random subset per poll instead of every slot on every poll.
func TestSemaphorePollsBoundedSubsets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		raw, _ := memory.New(256)
		t.Cleanup(func() { raw.Close() })
		backend := &countingAcquires{Backend: raw}
		m, _ := manager(t, backend, nil)
		semaphore, err := lease.DefineSemaphore("wide", keyspace.StringKeys[string](), 64).Bind(m)
		if err != nil {
			t.Fatal(err)
		}
		for range 64 {
			if _, ok, err := semaphore.Acquire(t.Context(), "resource", time.Hour, 0); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}
		backend.calls.Store(0)
		wait := 20 * lease.DefaultConfig(keyspace.Namespace{}).PollInterval
		if _, ok, err := semaphore.Acquire(t.Context(), "resource", time.Hour, wait); ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(ok, err)
		}
		// One full pass, then at most 8 slots for each of at most ~40 jittered polls.
		if calls := backend.calls.Load(); calls < 64 || calls > 64+8*41 {
			t.Fatal("semaphore polling was not bounded", calls)
		}
	})
}
