package cache_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestGetManyPreservesOrderMissesAndOwnership(t *testing.T) {
	s, backend, _ := store(t, func(c *cache.Config) { c.MaxBatchEntries = 4 })
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []userKey{"a", "c"} {
		if err := c.Put(t.Context(), name, profile{Name: string(name), Scores: []int{1}}, cache.Forever()); err != nil {
			t.Fatal(err)
		}
	}
	results, err := c.GetMany(t.Context(), "c", "b", "a", "c")
	if err != nil || len(results) != 4 {
		t.Fatal(results, err)
	}
	if !results[0].Found || results[0].Value.Name != "c" || results[1].Found || !results[2].Found || results[2].Value.Name != "a" || !results[3].Found {
		t.Fatal("batch results lost input order", results)
	}
	results[0].Value.Scores[0] = 9
	if results[3].Value.Scores[0] != 1 {
		t.Fatal("duplicate keys share a decoded value")
	}
	if empty, err := c.GetMany(t.Context()); err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	if _, err := c.GetMany(t.Context(), "a", "b", "c", "d", "e"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	// Adapters without the batch capability read each distinct key. Without
	// TaggedBackend the store uses plain addresses, so it writes its own entry.
	plain, err := cache.NewStore(struct{ cache.Backend }{backend}, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := profiles.Bind(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := fallback.Put(t.Context(), "a", profile{Name: "a"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if results, err := fallback.GetMany(t.Context(), "a", "x", "a"); err != nil || len(results) != 3 || !results[0].Found || results[0].Value.Name != "a" || results[1].Found || !results[2].Found {
		t.Fatal(results, err)
	}
}

func TestTaggedGetManyReResolvesAfterInvalidation(t *testing.T) {
	s, _, _ := store(t, nil)
	tags, _ := boundTags(t, s)
	view := taggedProfiles(t, s, tags.For("team"))
	if err := view.Put(t.Context(), "key", profile{Name: "old"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if results, err := view.GetMany(t.Context(), "key"); err != nil || !results[0].Found || results[0].Value.Name != "old" {
		t.Fatal(results, err)
	}
	if err := tags.Invalidate(t.Context(), "team"); err != nil {
		t.Fatal(err)
	}
	if results, err := view.GetMany(t.Context(), "key", "other"); err != nil || results[0].Found || results[1].Found {
		t.Fatal("invalidated entry was returned", results, err)
	}
}

func TestPullReadsAndRemoves(t *testing.T) {
	c := boundProfiles(t, nil)
	if err := c.Put(t.Context(), "notice", profile{Name: "once"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, found, err := c.Pull(t.Context(), "notice"); err != nil || !found || value.Name != "once" {
		t.Fatal(value, found, err)
	}
	if _, found, err := c.Pull(t.Context(), "notice"); err != nil || found {
		t.Fatal("pulled value remained", found, err)
	}
}

func TestPutManyEncodesFirstAndWritesInOrder(t *testing.T) {
	s, _, clock := store(t, func(c *cache.Config) { c.MaxBatchEntries = 3 })
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	err = c.PutMany(t.Context(), cache.For(time.Minute),
		cache.Entry[userKey, profile]{Key: "a", Value: profile{Name: "first"}},
		cache.Entry[userKey, profile]{Key: "b", Value: profile{Name: "b"}},
		cache.Entry[userKey, profile]{Key: "a", Value: profile{Name: "last"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	results, err := c.GetMany(t.Context(), "a", "b")
	if err != nil || !results[0].Found || results[0].Value.Name != "last" || !results[1].Found {
		t.Fatal("repeated key did not keep its last value", results, err)
	}
	if stats := s.Stats(); stats.Writes != 3 {
		t.Fatal("writes were not counted", stats)
	}
	clock.Advance(time.Minute)
	if results, err := c.GetMany(t.Context(), "a", "b"); err != nil || results[0].Found || results[1].Found {
		t.Fatal("batch TTL was not applied", results, err)
	}
	// A value that cannot be encoded writes nothing, including earlier entries.
	unencodable := profile{Attributes: map[string]any{"bad": make(chan int)}}
	err = c.PutMany(t.Context(), cache.Forever(), cache.Entry[userKey, profile]{Key: "c", Value: profile{Name: "c"}}, cache.Entry[userKey, profile]{Key: "d", Value: unencodable})
	if err == nil {
		t.Fatal("unencodable value was accepted")
	}
	if _, found, err := c.Get(t.Context(), "c"); err != nil || found {
		t.Fatal("codec failure wrote an earlier entry", found, err)
	}
	if err := c.PutMany(t.Context(), cache.Forever(), make([]cache.Entry[userKey, profile], 4)...); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := c.PutMany(t.Context(), cache.Forever()); err != nil {
		t.Fatal(err)
	}
}
