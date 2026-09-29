package cache_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

var departments = cache.DefineTag("departments", cache.StringKeys[userKey]())
var regions = cache.DefineTag("regions", cache.StringKeys[userKey]())

func boundTags(t *testing.T, s *cache.Store) (cache.Tags[userKey], cache.Tags[userKey]) {
	t.Helper()
	a, err := departments.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	b, err := regions.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}
func taggedProfiles(t *testing.T, s *cache.Store, tags ...cache.Tag) cache.Cache[userKey, profile] {
	t.Helper()
	base, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	view, err := base.WithTags(tags[0], tags[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func TestTagsCanonicalViewsInvalidateMatchingSetsOnly(t *testing.T) {
	s, _, clock := store(t, nil)
	a, b := boundTags(t, s)
	one := taggedProfiles(t, s, a.For("sales"), b.For("north"), a.For("sales"))
	same := taggedProfiles(t, s, b.For("north"), a.For("sales"))
	aOnly := taggedProfiles(t, s, a.For("sales"))
	other := taggedProfiles(t, s, a.For("service"))
	base, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []cache.Cache[userKey, profile]{one, aOnly, other, base} {
		if err := view.Put(t.Context(), "key", profile{Name: "saved", Scores: []int{1}}, cache.Forever()); err != nil {
			t.Fatal(err)
		}
	}
	if got, found, err := same.Get(t.Context(), "key"); err != nil || !found || got.Name != "saved" {
		t.Fatal(got, found, err)
	}
	if added, err := same.Add(t.Context(), "key", profile{Name: "replace"}, cache.For(time.Nanosecond)); err != nil || added {
		t.Fatal(added, err)
	}
	clock.Advance(time.Hour)
	if _, found, err := one.Get(t.Context(), "key"); err != nil || !found {
		t.Fatal("Add changed persistent expiry", found, err)
	}
	if err := a.Invalidate(t.Context(), "sales"); err != nil {
		t.Fatal(err)
	}
	for _, view := range []cache.Cache[userKey, profile]{one, same, aOnly} {
		if _, found, err := view.Get(t.Context(), "key"); err != nil || found {
			t.Fatal("tagged data survived invalidation", found, err)
		}
	}
	for _, view := range []cache.Cache[userKey, profile]{other, base} {
		if _, found, err := view.Get(t.Context(), "key"); err != nil || !found {
			t.Fatal("unrelated entry invalidated", found, err)
		}
	}
	if added, err := same.Add(t.Context(), "key", profile{Name: "new"}, cache.For(time.Second)); err != nil || !added {
		t.Fatal(added, err)
	}
	clock.Advance(time.Second)
	if _, found, err := one.Get(t.Context(), "key"); err != nil || found {
		t.Fatal("tagged expiry ignored", found, err)
	}
	if err := s.InvalidateTags(t.Context(), a.For("sales"), b.For("north")); err != nil {
		t.Fatal(err)
	}
}

func TestTaggedRememberSeparatesGenerationsAndRejectsStaleWriters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := store(t, nil)
		a, _ := boundTags(t, s)
		view := taggedProfiles(t, s, a.For("team"))
		release := make(chan struct{})
		old := asyncRemember(view, t.Context(), "key", func(context.Context) (profile, error) { <-release; return profile{Name: "old"}, nil })
		synctest.Wait()
		follower := asyncRemember(view, t.Context(), "key", noLoad)
		synctest.Wait()
		if err := a.Invalidate(t.Context(), "team"); err != nil {
			t.Fatal(err)
		}
		fresh, err := view.Remember(t.Context(), "key", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "new", Scores: []int{1}}, nil })
		if err != nil || fresh.Name != "new" {
			t.Fatal("new generation joined old loader", fresh, err)
		}
		close(release)
		// Callers that started before invalidation receive their loaded value,
		// but the stale publication is rejected and only reported.
		for _, ch := range []<-chan rememberResult{old, follower} {
			if got := <-ch; got.err != nil || got.value.Name != "old" {
				t.Fatal("stale loader result", got)
			}
		}
		if stats := s.Stats(); stats.WriteFailures != 1 {
			t.Fatal("stale publication was not rejected", stats)
		}
		fresh.Scores[0] = 9
		got, found, err := view.Get(t.Context(), "key")
		if err != nil || !found || got.Name != "new" || got.Scores[0] != 1 {
			t.Fatal("stale writer replaced new data or aliased snapshot", got, found, err)
		}
	})
}
func TestTaggedForeverDoesNotAccumulateGenerationKeys(t *testing.T) {
	s, backend, _ := store(t, nil)
	a, _ := boundTags(t, s)
	view := taggedProfiles(t, s, a.For("team"))
	for i := range 128 {
		if err := view.Put(t.Context(), "key", profile{Name: fmt.Sprint(i)}, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if stats := backend.Stats(); stats.Entries != 3 {
			t.Fatal("persistent generation entries accumulated", i, stats)
		}
		if err := a.Invalidate(t.Context(), "team"); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, err := view.Get(t.Context(), "key"); err != nil || found {
		t.Fatal(found, err)
	}
	if stats := backend.Stats(); stats.Entries != 2 {
		t.Fatal("stale entry not reclaimed on read", stats)
	}
}
func TestTaggedCounterKeepsAtomicArithmeticAndExpiry(t *testing.T) {
	s, _, clock := store(t, nil)
	a, _ := boundTags(t, s)
	base := boundCounters(t, s)
	view, err := base.WithTags(a.For("team"))
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 64 {
		group.Go(func() {
			if _, err := view.Increment(t.Context(), "key", 1, cache.For(time.Second)); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if got, found, err := view.Get(t.Context(), "key"); err != nil || !found || got != 64 {
		t.Fatal(got, found, err)
	}
	clock.Advance(time.Second)
	if got, err := view.Increment(t.Context(), "key", math.MaxInt64, cache.Forever()); err != nil || got != math.MaxInt64 {
		t.Fatal(got, err)
	}
	if _, err := view.Increment(t.Context(), "key", 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := a.Invalidate(t.Context(), "team"); err != nil {
		t.Fatal(err)
	}
	if got, err := view.Increment(t.Context(), "key", 1, cache.Forever()); err != nil || got != 1 {
		t.Fatal("stale counter not initialized from zero", got, err)
	}
	if _, found, err := base.Get(t.Context(), "key"); err != nil || found {
		t.Fatal("tagged counter overwrote untagged key", found, err)
	}
}
func TestTagsValidateOwnershipBoundsAndCallbackFailures(t *testing.T) {
	s, backend, _ := store(t, func(c *cache.Config) { c.MaxTags = 2 })
	a, b := boundTags(t, s)
	base, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := store(t, nil)
	foreign, _ := boundTags(t, other)
	for _, tag := range []cache.Tag{{}, foreign.For("team")} {
		if _, err := base.WithTags(tag); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := base.WithTags(a.For("a"), b.For("b"), a.For("c")); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := s.InvalidateTags(nil, a.For("a")); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := s.InvalidateTags(t.Context(), foreign.For("a")); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := cache.Define("departments", cache.StringKeys[userKey](), cache.JSON[int]()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal("tag family ownership ignored", err)
	}
	if _, err := departments.Bind(s); err != nil {
		t.Fatal("repeated declaration rejected", err)
	}
	unsupported, err := cache.NewStore(struct{ cache.Backend }{backend}, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := departments.Bind(unsupported); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var zero cache.Tags[userKey]
	if err := zero.Invalidate(t.Context(), "key"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		fn     func(userKey) (string, error)
		target error
	}{
		{"panic", func(userKey) (string, error) { panic("private tag detail") }, fault.Panicked},
		{"goexit", func(userKey) (string, error) { runtime.Goexit(); return "", nil }, fault.Panicked},
		{"invalid", func(userKey) (string, error) { return "bad\nkey", nil }, fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			tags, err := cache.DefineTag(cache.Name(test.name), cache.NewKeyCodec(test.fn)).Bind(s)
			if err != nil {
				t.Fatal(err)
			}
			view, err := base.WithTags(tags.For("key"))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := view.Get(t.Context(), "key"); !errors.Is(err, test.target) || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if err := tags.Invalidate(t.Context(), "key"); !errors.Is(err, test.target) || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.Invalidate(ctx, "team"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestInvalidTaggedWritesDoNotInitializeMetadata(t *testing.T) {
	s, backend, _ := store(t, nil)
	a, _ := boundTags(t, s)
	view := taggedProfiles(t, s, a.For("team"))
	if err := view.Put(t.Context(), "key", profile{}, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := view.Add(t.Context(), "key", profile{}, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := view.Remember(t.Context(), "key", cache.Forever(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	base := boundCounters(t, s)
	counter, err := base.WithTags(a.For("team"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := counter.Increment(t.Context(), "key", 1, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if stats := backend.Stats(); stats.Entries != 0 {
		t.Fatal("invalid operation initialized metadata", stats)
	}
}
