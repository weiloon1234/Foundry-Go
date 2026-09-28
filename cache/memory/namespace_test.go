package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestMemoryNamespaceContract(t *testing.T) {
	cachetest.RunNamespace(t, func(t *testing.T) cachetest.NamespaceFixture {
		backend, err := memory.New(memory.DefaultConfig(), testkit.NewClock(time.Now()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { backend.Close(context.Background()) })
		return cachetest.NamespaceFixture{Backend: backend, Namespace: cache.Namespace{Application: "namespace-test", Environment: "memory"}, Track: func(cache.EntryKey) {}}
	})
}
func TestNamespaceForeverMemoryRemainsBounded(t *testing.T) {
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024}, testkit.NewClock(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close(context.Background()) })
	store, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{Application: "forever", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := cache.Define("values", cache.StringKeys[string](), cache.JSON[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	for range 256 {
		if err := handle.Put(t.Context(), "key", "value", cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if err := store.Invalidate(t.Context()); err != nil {
			t.Fatal(err)
		}
		if stats := backend.Stats(); stats.Entries != 2 || stats.Evictions != 0 {
			t.Fatal("generation accumulated addresses", stats)
		}
	}
	if _, hit, err := handle.Get(t.Context(), "key"); err != nil || hit {
		t.Fatal(hit, err)
	}
	if stats := backend.Stats(); stats.Entries != 1 {
		t.Fatal("stale data was not reclaimed", stats)
	}
}
