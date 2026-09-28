package cachetest

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
	"time"
)

type BasicEntryBackend interface {
	Backend
	cache.EntryBackend
}

// RunBasicEntries covers optional inspection/expiry independently of tags.
func RunBasicEntries(t *testing.T, setup func(*testing.T) (BasicEntryBackend, func(string) cache.EntryKey)) {
	t.Helper()
	b, key := setup(t)
	k := key("entry")
	if hit, err := b.Exists(t.Context(), k); err != nil || hit {
		t.Fatal(hit, err)
	}
	if changed, err := b.Expire(t.Context(), k, cache.Forever()); err != nil || changed {
		t.Fatal("expiry created missing entry", changed, err)
	}
	if err := b.Put(t.Context(), k, []byte{}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if hit, err := b.Exists(t.Context(), k); err != nil || !hit {
		t.Fatal("empty entry missing", hit, err)
	}
	if changed, err := b.Expire(t.Context(), k, cache.For(time.Hour)); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if _, err := b.Expire(t.Context(), k, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := b.Exists(ctx, k); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := b.Expire(ctx, k, cache.Forever()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, hit, err := b.Get(t.Context(), k); err != nil || !hit {
		t.Fatal("invalid mutation lost entry", hit, err)
	}
	if _, err := b.Exists(t.Context(), cache.EntryKey{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
