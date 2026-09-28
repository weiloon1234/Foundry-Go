package cache_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestNamespaceInvalidationKeepsFullApplicationTagBudget(t *testing.T) {
	store, backend, _ := store(t, func(c *cache.Config) { c.MaxTags = cache.MaxTags; c.MaxDeclarations = 2 })
	values, err := profiles.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	declarations := cache.DefineTag("many", cache.StringKeys[string]())
	family, err := declarations.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	tags := make([]cache.Tag, cache.MaxTags)
	keys := make([]cache.EntryKey, 0, cache.MaxTags+1)
	for i := range tags {
		tags[i] = family.For(fmt.Sprint(i))
		key, err := cache.NewEntryKey(namespace, "many", fmt.Sprint(i))
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	view, err := values.WithTags(tags[0], tags[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	if err := view.Put(t.Context(), "key", profile{Name: "full tags"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := store.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := view.Get(t.Context(), "key"); err != nil || hit {
		t.Fatal(hit, err)
	}
	if _, err := values.WithTags(tags[0], tags...); !errors.Is(err, fault.Invalid) {
		t.Fatal("extra application tag accepted", err)
	}
	extra, err := cache.NewEntryKey(namespace, "many", "extra")
	if err != nil {
		t.Fatal(err)
	}
	keys = append(keys, extra)
	slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
	if _, err := backend.ResolveTags(t.Context(), keys); !errors.Is(err, fault.Invalid) {
		t.Fatal("reserved slot used by application tag", err)
	}
}

type namespaceFailure struct {
	cache.Backend
	cache.TaggedBackend
	fail func(context.Context) error
}

func (b namespaceFailure) InvalidateTags(ctx context.Context, _ []cache.EntryKey) error {
	return b.fail(ctx)
}
func TestNamespaceInvalidationRejectsInvalidAndUnsupportedOperations(t *testing.T) {
	s, backend, _ := store(t, nil)
	var zero *cache.Store
	for _, current := range []*cache.Store{zero, s} {
		if err := current.Invalidate(nil); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Invalidate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	basic, err := cache.NewStore(struct{ cache.Backend }{backend}, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	if err := basic.Invalidate(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("basic adapter claimed invalidation", err)
	}
	incomplete := struct {
		cache.Backend
		cache.TaggedBackend
		cache.CounterBackend
	}{backend, backend, backend}
	limited, err := cache.NewStore(incomplete, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.DefineCounter("unsupported", cache.StringKeys[string]()).Bind(limited); !errors.Is(err, fault.Invalid) {
		t.Fatal("counter silently bypassed namespace snapshot", err)
	}
	if stats := backend.Stats(); stats.Entries != 0 {
		t.Fatal("invalid operation initialized metadata", stats)
	}
}
func TestNamespaceInvalidationOwnsCallbacksAndFailures(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			_, backend, _ := store(t, nil)
			cause := errors.New("private invalidation detail")
			calls := 0
			wrapped := namespaceFailure{Backend: backend, TaggedBackend: backend, fail: func(ctx context.Context) error {
				calls++
				switch mode {
				case "panic":
					panic(cause)
				case "goexit":
					runtime.Goexit()
				case "timeout":
					<-ctx.Done()
					return nil
				}
				return cause
			}}
			config := cache.DefaultConfig(namespace)
			config.Timeout = time.Millisecond
			s, err := cache.NewStore(wrapped, config)
			if err != nil {
				t.Fatal(err)
			}
			err = s.Invalidate(t.Context())
			target := cause
			switch mode {
			case "panic", "goexit":
				target = fault.Panicked
			case "timeout":
				target = context.DeadlineExceeded
			}
			if !errors.Is(err, target) || calls != 1 || strings.Contains(err.Error(), "private") {
				t.Fatal(err, calls)
			}
		})
	}
	synctest.Test(t, func(t *testing.T) {
		_, backend, _ := store(t, nil)
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		wrapped := namespaceFailure{Backend: backend, TaggedBackend: backend, fail: func(ctx context.Context) error { close(entered); <-release; return nil }}
		config := cache.DefaultConfig(namespace)
		config.Timeout = time.Second
		s, err := cache.NewStore(wrapped, config)
		if err != nil {
			t.Fatal(err)
		}
		go func() { done <- s.Invalidate(t.Context()) }()
		<-entered
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("invalidation detached a live callback")
		default:
		}
		close(release)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}

func TestReservedNamespaceMetadataCannotBecomePayload(t *testing.T) {
	scope, err := cache.NewNamespaceTagKey(namespace)
	if err != nil {
		t.Fatal(err)
	}
	version, err := cache.NewTagVersion()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.NewTaggedKey(scope, []cache.TagStamp{{Key: scope, Version: version}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("reserved metadata became a derived payload", err)
	}
	if _, err := cache.NewNamespaceTagKey(cache.Namespace{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
