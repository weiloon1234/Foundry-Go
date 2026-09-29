package cache_test

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// awaitFresh polls until Flexible returns want. Value publication precedes the
// freshness marker and fill release, so seeing want does not prove the fill has
// finished. A stale snapshot can also elect a later fill that rechecks storage.
func awaitFresh(t *testing.T, c cache.Cache[userKey, profile], want string, loader func(context.Context) (profile, error)) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, loader)
		if err != nil {
			t.Fatal(err)
		}
		if value.Name == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh did not publish", value)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFlexibleServesStaleWhileRefreshingOnce(t *testing.T) {
	synctest.Test(t, testFlexibleServesStaleWhileRefreshingOnce)
}

func testFlexibleServesStaleWhileRefreshingOnce(t *testing.T) {
	s, _, clock := store(t, nil)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	load := func(name string) func(context.Context) (profile, error) {
		return func(context.Context) (profile, error) { calls.Add(1); return profile{Name: name}, nil }
	}
	value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, load("first"))
	if err != nil || value.Name != "first" || calls.Load() != 1 {
		t.Fatal(value, err, calls.Load())
	}
	// Fresh: no loader runs.
	if value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, load("unused")); err != nil || value.Name != "first" || calls.Load() != 1 {
		t.Fatal(value, err, calls.Load())
	}
	// Stale: every caller gets the old value at once; one background fill runs.
	clock.Advance(2 * time.Minute)
	release, entered, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	refresh := func(ctx context.Context) (profile, error) {
		defer close(finished)
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return profile{Name: "second"}, nil
		case <-ctx.Done():
			return profile{}, ctx.Err()
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	if value, err := c.Flexible(canceled, "key", time.Minute, time.Hour, refresh); err != nil || value.Name != "first" {
		t.Fatal("stale value was not served", value, err)
	}
	// The refresh does not depend on the request that triggered it.
	cancel()
	<-entered
	for range 3 {
		if value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, load("duplicate")); err != nil || value.Name != "first" {
			t.Fatal(value, err)
		}
	}
	close(release)
	<-finished
	awaitFresh(t, c, "second", load("unexpected"))
	// Finish publication and any elected storage recheck before advancing the
	// backend clock; otherwise pending work would correctly observe the expiry.
	synctest.Wait()
	if calls.Load() != 2 {
		t.Fatal("stale reads did not coalesce into one refresh", calls.Load())
	}
	if stats := s.Stats(); stats.Loads != 2 || stats.Writes != 2 {
		t.Fatal(stats)
	}
	// Past fresh+stale the value expired: the caller loads synchronously.
	clock.Advance(2 * time.Hour)
	if value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, load("third")); err != nil || value.Name != "third" || calls.Load() != 3 {
		t.Fatal(value, err, calls.Load())
	}
}

func TestFlexibleRefreshFailureKeepsStaleValue(t *testing.T) {
	s, _, clock := store(t, nil)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, func(context.Context) (profile, error) { return profile{Name: "kept"}, nil }); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Minute)
	failed := make(chan struct{})
	value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, func(context.Context) (profile, error) {
		defer close(failed)
		return profile{}, errors.New("source unavailable")
	})
	if err != nil || value.Name != "kept" {
		t.Fatal("refresh failure reached the caller", value, err)
	}
	<-failed
	// The failed refresh leaves the value stale; a later read refreshes again.
	refreshed := make(chan struct{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, func(context.Context) (profile, error) {
			select {
			case <-refreshed:
			default:
				close(refreshed)
			}
			return profile{Name: "recovered"}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if value.Name == "recovered" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stale value was never refreshed after a failure")
		}
		time.Sleep(time.Millisecond)
	}
	// Plain reads see the value as an ordinary entry of the family.
	if value, found, err := c.Get(t.Context(), "key"); err != nil || !found || value.Name != "recovered" {
		t.Fatal(value, found, err)
	}
}

func TestFlexibleValidatesInputs(t *testing.T) {
	c := boundProfiles(t, nil)
	loader := func(context.Context) (profile, error) { return profile{}, nil }
	for _, durations := range [][2]time.Duration{{0, time.Minute}, {time.Minute, 0}, {-time.Second, time.Minute}, {math.MaxInt64, time.Second}} {
		if _, err := c.Flexible(t.Context(), "key", durations[0], durations[1], loader); !errors.Is(err, fault.Invalid) {
			t.Fatal(durations, err)
		}
	}
	if _, err := c.Flexible(t.Context(), "key", time.Minute, time.Minute, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.Flexible(t.Context(), "key", time.Minute, time.Minute, loader, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

// Close stops new operations, cancels running fills and waits until an
// in-flight background refresh (and its publication) has actually exited.
func TestStoreCloseDrainsInFlightRefresh(t *testing.T) {
	s, _, clock := store(t, nil)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, func(context.Context) (profile, error) { return profile{Name: "old"}, nil }); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Minute)
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var canceled atomic.Bool
	refresh := func(ctx context.Context) (profile, error) {
		defer close(exited)
		close(entered)
		<-release // ignores cancellation on purpose
		canceled.Store(ctx.Err() != nil)
		return profile{Name: "new"}, nil
	}
	if value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, refresh); err != nil || value.Name != "old" {
		t.Fatal(value, err)
	}
	<-entered
	bounded, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.Close(bounded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Close returned before the refresh exited", err)
	}
	select {
	case <-s.Done():
		t.Fatal("store reported drained while a refresh was running")
	default:
	}
	if _, _, err := c.Get(t.Context(), "key"); !errors.Is(err, fault.Closed) {
		t.Fatal("closed store admitted an operation", err)
	}
	if _, err := c.Remember(t.Context(), "other", cache.Forever(), func(context.Context) (profile, error) { return profile{}, nil }); !errors.Is(err, fault.Closed) {
		t.Fatal("closed store admitted a fill", err)
	}
	close(release)
	<-exited
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	if !canceled.Load() {
		t.Fatal("Close did not cancel the running fill's context")
	}
	// The canceled refresh publishes nothing after Close.
	if stats := s.Stats(); stats.Writes != 1 {
		t.Fatal("a canceled refresh was published", stats)
	}
}

// Plain writes leave the freshness marker as it is.
func TestFlexibleMarkerIsIndependentOfPlainWrites(t *testing.T) {
	s, _, clock := store(t, nil)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	load := func(context.Context) (profile, error) { calls.Add(1); return profile{Name: "loaded"}, nil }
	if _, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, load); err != nil {
		t.Fatal(err)
	}
	// Written while the marker lives: fresh, no refresh.
	if err := c.Put(t.Context(), "key", profile{Name: "written"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, load); err != nil || value.Name != "written" || calls.Load() != 1 {
		t.Fatal(value, err, calls.Load())
	}
	// Written after the marker expired: served stale, then refreshed.
	clock.Advance(2 * time.Minute)
	if err := c.Put(t.Context(), "key", profile{Name: "late"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, err := c.Flexible(t.Context(), "key", time.Minute, time.Hour, load); err != nil || value.Name != "late" {
		t.Fatal(value, err)
	}
	awaitFresh(t, c, "loaded", load)
	if calls.Load() != 2 {
		t.Fatal("stale write was not refreshed exactly once", calls.Load())
	}
}
