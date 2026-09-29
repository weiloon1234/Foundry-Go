package cache_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
)

// countingReads counts tagged payload reads reaching the adapter.
type countingReads struct {
	*memory.Backend
	reads atomic.Int32
}

func (b *countingReads) GetTagged(ctx context.Context, key cache.TaggedKey) ([]byte, bool, error) {
	b.reads.Add(1)
	return b.Backend.GetTagged(ctx, key)
}
func (b *countingReads) GetManyTagged(ctx context.Context, keys []cache.TaggedKey) ([]cache.BatchValue, error) {
	b.reads.Add(int32(len(keys)))
	return b.Backend.GetManyTagged(ctx, keys)
}

func TestMemoAnswersRepeatedReadsWithinOneContext(t *testing.T) {
	_, backend, _ := store(t, nil)
	counted := &countingReads{Backend: backend}
	s, err := cache.NewStore(counted, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "a", profile{Name: "a", Scores: []int{1}}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	request := cache.WithMemo(t.Context())
	if cache.WithMemo(request) != request {
		t.Fatal("nested memo replaced the outer memo")
	}
	first, found, err := c.Get(request, "a")
	if err != nil || !found || first.Name != "a" || counted.reads.Load() != 1 {
		t.Fatal(first, found, err, counted.reads.Load())
	}
	first.Scores[0] = 9
	// Another request changes the entry; this request keeps its memoized result.
	if err := c.Put(t.Context(), "a", profile{Name: "changed"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	second, found, err := c.Get(request, "a")
	if err != nil || !found || second.Name != "a" || second.Scores[0] != 1 || counted.reads.Load() != 1 {
		t.Fatal("memoized read was not reused or shared a decoded value", second, found, err, counted.reads.Load())
	}
	if exists, err := c.Exists(request, "a"); err != nil || !exists {
		t.Fatal(exists, err)
	}
	remembered, err := c.Remember(request, "a", cache.Forever(), func(context.Context) (profile, error) { return profile{}, fmt.Errorf("not called") })
	if err != nil || remembered.Name != "a" || counted.reads.Load() != 1 {
		t.Fatal(remembered, err, counted.reads.Load())
	}
	// A miss is memoized for Get, but Remember still consults the Store.
	if _, found, err := c.Get(request, "missing"); err != nil || found {
		t.Fatal(found, err)
	}
	if err := c.Put(t.Context(), "missing", profile{Name: "elsewhere"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, found, err := c.Get(request, "missing"); err != nil || found {
		t.Fatal("memoized miss was not reused", found, err)
	}
	if value, err := c.Remember(request, "missing", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "loaded"}, nil }); err != nil || value.Name != "elsewhere" {
		t.Fatal(value, err)
	}
	// Writes through the memo context forget the key.
	if err := c.Put(request, "a", profile{Name: "written"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, _, err := c.Get(request, "a"); err != nil || value.Name != "written" {
		t.Fatal("write did not forget the memoized value", value, err)
	}
	// GetMany answers memoized keys and reads the rest in one batch.
	before := counted.reads.Load()
	results, err := c.GetMany(request, "a", "b", "missing")
	if err != nil || !results[0].Found || results[0].Value.Name != "written" || results[1].Found || !results[2].Found || counted.reads.Load() != before+1 {
		t.Fatal(results, err, counted.reads.Load()-before)
	}
	// Invalidation through the context forgets the store's memoized results.
	if err := s.Invalidate(request); err != nil {
		t.Fatal(err)
	}
	if _, found, err := c.Get(request, "a"); err != nil || found {
		t.Fatal("invalidation kept a memoized value", found, err)
	}
	// Without a memo every read reaches the Store.
	before = counted.reads.Load()
	for range 2 {
		if _, _, err := c.Get(t.Context(), "a"); err != nil {
			t.Fatal(err)
		}
	}
	if counted.reads.Load() != before+2 {
		t.Fatal("reads without a memo were memoized")
	}
}

func TestMemoIsBoundedAndSeparatesViews(t *testing.T) {
	s, _, _ := store(t, nil)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	tags, _ := boundTags(t, s)
	view := taggedProfiles(t, s, tags.For("team"))
	request := cache.WithMemo(t.Context())
	if err := view.Put(request, "k", profile{Name: "tagged"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, found, err := view.Get(request, "k"); err != nil || !found || value.Name != "tagged" {
		t.Fatal(value, found, err)
	}
	if _, found, err := c.Get(request, "k"); err != nil || found {
		t.Fatal("untagged handle reused the tagged view's memo", found, err)
	}
	for i := range cache.MaxMemoEntries {
		if _, _, err := c.Get(request, userKey(fmt.Sprint("fill-", i))); err != nil {
			t.Fatal(err)
		}
	}
	// The memo is full: this key is read from the Store every time.
	if _, found, err := c.Get(request, "late"); err != nil || found {
		t.Fatal(found, err)
	}
	if err := c.Put(t.Context(), "late", profile{Name: "late"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, found, err := c.Get(request, "late"); err != nil || !found || value.Name != "late" {
		t.Fatal("a full memo recorded another result", value, found, err)
	}
}
