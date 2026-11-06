package sourcemap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneAgeCurrentAndUnknownPreservation(t *testing.T) {
	c := testConfig(t)
	old := artifact(t, c, "old", "admin", []byte("old"), 2*time.Hour)
	artifact(t, c, "old", "user", []byte("old user"), 2*time.Hour)
	current := artifact(t, c, "current", "admin", []byte("current"), 3*time.Hour)
	fresh := artifact(t, c, "fresh", "admin", []byte("fresh"), 0)
	unknown := artifact(t, c, "unknown", "admin", []byte("map"), 3*time.Hour)
	writeFile(t, filepath.Join(unknown, "notes.txt"), []byte("unowned"))
	var expected int64
	for _, portal := range []string{"admin", "user"} {
		for _, file := range []string{"manifest.json", "app.js.map"} {
			info, err := os.Stat(filepath.Join(c.Directory, "old", portal, file))
			if err != nil {
				t.Fatal(err)
			}
			expected += info.Size()
		}
	}
	store := newStore(t, c)
	result, err := store.Prune(context.Background())
	if err != nil || result.Releases != 1 || result.Bytes != expected {
		t.Fatalf("result %+v: %v", result, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old retained: %v", err)
	}
	for _, dir := range []string{current, fresh, unknown} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("lost retained release: %v", err)
		}
	}
	result, err = store.Prune(context.Background())
	if err != nil || result != (PruneResult{}) {
		t.Fatalf("repeat %+v: %v", result, err)
	}
}

func TestPruneStorageOldestAndActiveCap(t *testing.T) {
	c := testConfig(t)
	c.StorageBytes = 1024
	artifact(t, c, "older", "admin", make([]byte, 600), time.Minute*30)
	artifact(t, c, "newer", "admin", make([]byte, 600), time.Minute*10)
	store := newStore(t, c)
	result, err := store.Prune(context.Background())
	if err != nil || result.Releases != 1 {
		t.Fatalf("result %+v: %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(c.Directory, "older")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("did not prune oldest")
	}
	if _, err := os.Stat(filepath.Join(c.Directory, "newer")); err != nil {
		t.Fatal("removed newest")
	}
	artifact(t, c, "current", "admin", make([]byte, 1024), 2*time.Hour)
	result, err = store.Prune(context.Background())
	if err == nil || result.Releases != 1 {
		t.Fatalf("expected cap excess and progress: %+v %v", result, err)
	}
	if _, err := store.Read(context.Background(), "current", "admin/_assets/app.js"); err != nil {
		t.Fatalf("active map lost: %v", err)
	}
}

func TestPruneIncompleteAndUnknownPortals(t *testing.T) {
	for _, kind := range []string{"missing map", "unknown portal", "empty release", "missing manifest", "unknown file"} {
		t.Run(kind, func(t *testing.T) {
			c := testConfig(t)
			dir := artifact(t, c, "old", "admin", nil, 2*time.Hour)
			switch kind {
			case "missing map":
				if err := os.Remove(filepath.Join(dir, "app.js.map")); err != nil {
					t.Fatal(err)
				}
			case "missing manifest":
				if err := os.Remove(filepath.Join(dir, "manifest.json")); err != nil {
					t.Fatal(err)
				}
			case "unknown portal":
				if err := os.Mkdir(filepath.Join(c.Directory, "old", "other"), 0700); err != nil {
					t.Fatal(err)
				}
			case "unknown file":
				writeFile(t, filepath.Join(dir, "unknown"), nil)
			case "empty release":
				if err := os.Mkdir(filepath.Join(c.Directory, "empty"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(dir, "manifest.json")); err != nil {
					t.Fatal(err)
				}
			}
			result, err := newStore(t, c).Prune(context.Background())
			if err != nil || result != (PruneResult{}) {
				t.Fatalf("pruned incomplete release: %+v %v", result, err)
			}
			if _, err := os.Stat(filepath.Join(c.Directory, "old")); err != nil {
				t.Fatal("lost incomplete release")
			}
		})
	}
}

func TestPruneInventoryBoundsBeforeDeletion(t *testing.T) {
	for _, kind := range []string{"releases", "portals", "files", "aggregate"} {
		t.Run(kind, func(t *testing.T) {
			c := testConfig(t)
			dir := artifact(t, c, "old", "admin", nil, 2*time.Hour)
			switch kind {
			case "releases":
				for i := 0; i < maxReleases; i++ {
					if err := os.Mkdir(filepath.Join(c.Directory, fmt.Sprintf("r%d", i)), 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "portals":
				for i := 0; i < maxPortals; i++ {
					if err := os.Mkdir(filepath.Join(c.Directory, "old", fmt.Sprintf("p%d", i)), 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "files":
				for i := 0; i < maxFiles; i++ {
					writeFile(t, filepath.Join(dir, fmt.Sprintf("file%d", i)), nil)
				}
			}
			store := newStore(t, c)
			var err error
			if kind == "aggregate" {
				root, openErr := os.OpenRoot(c.Directory)
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer root.Close()
				budget := 1
				_, _, err = store.inspectRelease(context.Background(), root, "old", &budget)
			} else {
				_, err = store.Prune(context.Background())
			}
			if err == nil {
				t.Fatal("accepted oversized inventory")
			}
			if _, err := os.Stat(filepath.Join(dir, "app.js.map")); err != nil {
				t.Fatal("deleted before completed inventory")
			}
		})
	}
}

func TestPruneChangedInventoryPreservesFiles(t *testing.T) {
	for _, kind := range []string{"replacement", "modified", "unknown added", "manifest"} {
		t.Run(kind, func(t *testing.T) {
			c := testConfig(t)
			dir := artifact(t, c, "old", "admin", []byte("old"), 2*time.Hour)
			store := newStore(t, c)
			root, err := os.OpenRoot(c.Directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			budget := maxInventory
			before, owned, err := store.inspectRelease(context.Background(), root, "old", &budget)
			if err != nil || !owned {
				t.Fatalf("inspection: %v", err)
			}
			name := filepath.Join(dir, "app.js.map")
			switch kind {
			case "replacement":
				writeFile(t, filepath.Join(dir, "replacement"), []byte("new"))
				if err := os.Rename(filepath.Join(dir, "replacement"), name); err != nil {
					t.Fatal(err)
				}
			case "modified":
				writeFile(t, name, []byte("changed map"))
			case "unknown added":
				writeFile(t, filepath.Join(dir, "unknown"), []byte("preserve"))
			case "manifest":
				writeFile(t, filepath.Join(dir, "manifest.json"), []byte("{}"))
			}
			if err := store.removeRelease(context.Background(), root, before); err == nil {
				t.Fatal("deleted changed release")
			}
			if _, err := os.Stat(name); err != nil {
				t.Fatal("lost changed map")
			}
		})
	}
}

// Invoke a filesystem change or cancellation at a deterministic read boundary.
type actionContext struct {
	context.Context
	calls  int
	at     int
	action func()
}

func (c *actionContext) Err() error {
	c.calls++
	if c.calls == c.at {
		c.action()
	}
	return c.Context.Err()
}

func TestReadChangedFileAndMidReadCancellation(t *testing.T) {
	for _, kind := range []string{"replacement", "modified", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			c := testConfig(t)
			dir := artifact(t, c, "old", "admin", []byte("old"), 0)
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &actionContext{Context: base, at: 2, action: func() {
				name := filepath.Join(dir, "app.js.map")
				switch kind {
				case "replacement":
					writeFile(t, filepath.Join(dir, "replacement"), []byte("new"))
					if err := os.Rename(filepath.Join(dir, "replacement"), name); err != nil {
						t.Fatal(err)
					}
				case "modified":
					writeFile(t, name, []byte("longer data"))
				case "cancel":
					cancel()
				}
			}}
			data, err := readPrivate(ctx, root, "app.js.map", 1024)
			if err == nil || data != nil {
				t.Fatalf("returned changed/canceled data: %v", err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestPruneCancellationAfterInventory(t *testing.T) {
	c := testConfig(t)
	dir := artifact(t, c, "old", "admin", nil, 2*time.Hour)
	store := newStore(t, c)
	root, err := os.OpenRoot(c.Directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	budget := maxInventory
	before, owned, err := store.inspectRelease(context.Background(), root, "old", &budget)
	if err != nil || !owned {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.removeRelease(ctx, root, before); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "app.js.map")); err != nil {
		t.Fatal("deleted canceled release")
	}
}
