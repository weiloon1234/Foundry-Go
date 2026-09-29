package file_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/file"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func key(t *testing.T, text string) cache.EntryKey {
	t.Helper()
	k, err := cache.NewEntryKey(cache.Namespace{Application: "file-contract", Environment: "test"}, "values", text)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func open(t *testing.T, c file.Config) *file.Backend {
	t.Helper()
	b, err := file.Open(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return b
}
func TestSharedCacheContract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) (cachetest.Backend, func(string) cache.EntryKey) {
		b := open(t, file.DefaultConfig(t.TempDir()))
		return b, func(s string) cache.EntryKey { return key(t, s) }
	})
}
func TestExpiryReopenCapacityAndOwnedRoot(t *testing.T) {
	source := testkit.NewClock(time.Now())
	c := file.DefaultConfig(t.TempDir())
	c.Clock = source
	c.MaxEntries = 1
	first := open(t, c)
	k := key(t, "value")
	if err := first.Put(t.Context(), k, []byte("old"), cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Add(t.Context(), key(t, "other"), []byte("new"), cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if got, _, _ := first.Get(t.Context(), k); string(got) != "old" {
		t.Fatal("capacity changed live entry")
	}
	if changed, err := first.Expire(t.Context(), k, cache.For(2*time.Second)); err != nil || !changed {
		t.Fatal(changed, err)
	}
	second := open(t, c)
	source.Advance(3 * time.Second)
	if hit, err := second.Exists(t.Context(), k); err != nil || hit {
		t.Fatal(hit, err)
	}
	if count, err := second.Prune(t.Context(), 1); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := second.Put(t.Context(), key(t, "other"), nil, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.Get(t.Context(), k); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	path := filepath.Join(foreign, "owned-by-user")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Open(t.Context(), file.DefaultConfig(foreign)); err == nil {
		t.Fatal("unrelated root accepted")
	}
	entries, _ := os.ReadDir(foreign)
	if len(entries) != 1 {
		t.Fatal("unrelated root changed")
	}
}
func TestProcessAtomicCounters(t *testing.T) {
	if root := os.Getenv("FOUNDRY_FILE_CACHE_CHILD_ROOT"); root != "" {
		b := open(t, file.DefaultConfig(root))
		for range 16 {
			if _, err := b.Increment(t.Context(), key(t, "process"), 1, cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	root := t.TempDir()
	b := open(t, file.DefaultConfig(root))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestProcessAtomicCounters$")
			cmd.Env = append(os.Environ(), "FOUNDRY_FILE_CACHE_CHILD_ROOT="+root)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("child: %v %s", err, output)
			}
		})
	}
	wg.Wait()
	if got, err := b.Increment(t.Context(), key(t, "process"), 0, cache.Forever()); err != nil || got != 64 {
		t.Fatal(got, err)
	}
}
func TestSharedEntryContract(t *testing.T) {
	cachetest.RunBasicEntries(t, func(t *testing.T) (cachetest.BasicEntryBackend, func(string) cache.EntryKey) {
		b := open(t, file.DefaultConfig(t.TempDir()))
		return b, func(s string) cache.EntryKey { return key(t, s) }
	})
}

// records returns the paths of the record files under root.
func records(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".cache" {
			paths = append(paths, filepath.Join(root, entry.Name()))
		}
	}
	return paths
}
func TestCorruptRecordIsARemovableMiss(t *testing.T) {
	root := t.TempDir()
	source := testkit.NewClock(time.Now())
	c := file.DefaultConfig(root)
	c.Clock = source
	b := open(t, c)
	k := key(t, "corrupt")
	if err := b.Put(t.Context(), k, []byte("payload"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	paths := records(t, root)
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	path := paths[0]
	corrupt := func() []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data[len(data)-1] ^= 1
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return data
	}
	data := corrupt()
	if _, hit, err := b.Get(t.Context(), k); err != nil || hit {
		t.Fatal("corrupt record read", hit, err)
	}
	if found, err := b.Exists(t.Context(), k); err != nil || found {
		t.Fatal("corrupt existence accepted", found, err)
	}
	if changed, err := b.Expire(t.Context(), k, cache.For(time.Second)); err != nil || changed {
		t.Fatal("corrupt expiry accepted", changed, err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(data) {
		t.Fatal("expiry changed a corrupt record", err)
	}
	// A pass validates envelopes only: it skips and counts records with a
	// corrupt header while it reclaims other expiry. Payload checksums are
	// verified by reads, which treat a mismatch as a miss.
	if err := os.WriteFile(path, append([]byte("BROKEN!!"), data[8:]...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := b.Get(t.Context(), k); err != nil || hit {
		t.Fatal("corrupt envelope read", hit, err)
	}
	if err := b.Put(t.Context(), key(t, "expired"), nil, cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "foreign"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	source.Advance(2 * time.Second)
	result, err := b.Sweep(t.Context(), file.MaxPrune)
	if err != nil || result.Removed != 1 || result.Corrupt != 1 || result.Unrecognized != 1 || result.Entries != 1 || !result.Complete {
		t.Fatalf("%+v %v", result, err)
	}
	if err := b.Put(t.Context(), k, []byte("replacement"), cache.Forever()); err != nil {
		t.Fatal("corrupt record was not replaceable", err)
	}
	if got, hit, err := b.Get(t.Context(), k); err != nil || !hit || string(got) != "replacement" {
		t.Fatal(string(got), hit, err)
	}
	corrupt()
	if value, err := b.Increment(t.Context(), k, 3, cache.Forever()); err != nil || value != 3 {
		t.Fatal("corrupt record blocked increment", value, err)
	}
	corrupt()
	if removed, err := b.Forget(t.Context(), k); err != nil || removed {
		t.Fatal("corrupt record reported as live", removed, err)
	}
	if paths := records(t, root); len(paths) != 0 {
		t.Fatal("corrupt record was not removed", paths)
	}
}
func TestPersistentContract(t *testing.T) {
	cachetest.RunPersistent(t, func(t *testing.T) cachetest.PersistentFixture {
		root := t.TempDir()
		source := testkit.NewClock(time.Now())
		return cachetest.PersistentFixture{
			Open: func(t *testing.T, maxEntries, maxValueBytes int) cachetest.PersistentBackend {
				c := file.DefaultConfig(root)
				c.Clock = source
				c.MaxEntries = maxEntries
				c.MaxValueBytes = maxValueBytes
				c.PruneInterval = 0
				return open(t, c)
			},
			Clock: source,
			Key: func(namespace cache.Namespace, logical string) cache.EntryKey {
				k, err := cache.NewEntryKey(namespace, "values", logical)
				if err != nil {
					t.Fatal(err)
				}
				return k
			},
		}
	})
}
func TestAutomaticPruningFollowsLifecycle(t *testing.T) {
	root := t.TempDir()
	source := testkit.NewClock(time.Now())
	c := file.DefaultConfig(root)
	c.Clock = source
	c.PruneInterval = time.Second
	b := open(t, c)
	if err := b.Put(t.Context(), key(t, "expiring"), []byte("value"), cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	source.Advance(2 * time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for len(records(t, root)) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("started backend did not prune")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatal("close did not stop the pruner", err)
	}
	select {
	case <-b.Done():
	default:
		t.Fatal("closed backend is not done")
	}
}
func TestPruneIntervalValidation(t *testing.T) {
	c := file.DefaultConfig(t.TempDir())
	if c.PruneInterval != time.Minute {
		t.Fatal(c.PruneInterval)
	}
	for interval, valid := range map[time.Duration]bool{0: true, time.Second: true, 24 * time.Hour: true, -time.Second: false, time.Millisecond: false, 25 * time.Hour: false} {
		c.PruneInterval = interval
		if err := c.Validate(); (err == nil) != valid {
			t.Fatal(interval, err)
		}
	}
}
func TestCloseCancelsContendedStartup(t *testing.T) {
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	release, err := filelock.Acquire(t.Context(), root, ".foundry-cache.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	b, err := file.Prepare(file.DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { started <- b.Start(context.Background()) }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	// Close may win before Start enters, or cancel it while it waits for the lock.
	if err = b.Close(ctx); err != nil {
		t.Fatal("close blocked on startup lock", err)
	}
	select {
	case err := <-started:
		if err == nil {
			t.Fatal("contended startup unexpectedly succeeded")
		}
	case <-ctx.Done():
		t.Fatal("startup did not stop")
	}
}

func TestZeroBackendHasNoLifecycle(t *testing.T) {
	var b file.Backend
	if err := b.Start(t.Context()); err == nil {
		t.Fatal("zero backend started")
	}
	if err := b.Close(t.Context()); err == nil {
		t.Fatal("zero backend accepted lifecycle")
	}
}
