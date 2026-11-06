package sourcemap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/telemetry"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{Directory: t.TempDir(), Release: "current", Portals: []telemetry.Portal{"admin", "user"}, MaxBytes: 1024, Retention: time.Hour, StorageBytes: 1 << 20, Clock: fixedClock{time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}}
}
func newStore(t *testing.T, config Config) *Store {
	t.Helper()
	store, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func writeFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func artifact(t *testing.T, config Config, release, portal string, data []byte, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(config.Directory, release, portal)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(sourceMapManifest{Release: release, Portal: portal, Assets: map[string]string{"_assets/app.js": "app.js.map"}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
	writeFile(t, filepath.Join(dir, "app.js.map"), data)
	at := config.Clock.Now().Add(-age)
	for _, name := range []string{"manifest.json", "app.js.map"} {
		if err := os.Chtimes(filepath.Join(dir, name), at, at); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseAsset(t *testing.T) {
	for _, text := range []string{"admin/_assets/app.js", "A-1/_assets/A.b_c-1.js"} {
		got, err := ParseAsset(text)
		if err != nil || string(got) != text {
			t.Fatalf("%q: %v", text, err)
		}
	}
	for _, text := range []string{"", "../_assets/app.js", "admin/_assets/../app.js", "admin/_assets/%2e%2e", "admin/_assets/%252e", "admin\\_assets\\app.js", "admin//_assets/app.js", "admin/_assets/app.js?x=1", "https://host/admin/_assets/app.js", "/admin/_assets/app.js", "admin/_assets/a..js", "admin/_assets/é.js", "admin/_assets/" + strings.Repeat("a", 129)} {
		if _, err := ParseAsset(text); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestConfigBoundsAndNoIO(t *testing.T) {
	config := testConfig(t)
	config.Directory = filepath.Join(config.Directory, "does-not-exist")
	store := newStore(t, config)
	if _, err := os.Stat(config.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor touched directory: %v", err)
	}
	config.Portals[0] = "changed"
	if store.config.Portals[0] != "admin" {
		t.Fatal("configuration was not snapshotted")
	}
	for name, change := range map[string]func(*Config){
		"release":           func(c *Config) { c.Release = "../bad" },
		"map zero":          func(c *Config) { c.MaxBytes = 0 },
		"map cap":           func(c *Config) { c.MaxBytes = 10<<20 + 1 },
		"short retention":   func(c *Config) { c.Retention = time.Hour - 1 },
		"long retention":    func(c *Config) { c.Retention = 30*24*time.Hour + 1 },
		"small storage":     func(c *Config) { c.StorageBytes = int64(c.MaxBytes) - 1 },
		"large storage":     func(c *Config) { c.StorageBytes = 1<<30 + 1 },
		"empty portals":     func(c *Config) { c.Portals = nil },
		"duplicate portals": func(c *Config) { c.Portals = []telemetry.Portal{"admin", "admin"} },
		"invalid portal":    func(c *Config) { c.Portals = []telemetry.Portal{"../admin"} },
		"many portals":      func(c *Config) { c.Portals = make([]telemetry.Portal, 33) },
		"clock":             func(c *Config) { c.Clock = nil },
		"typed nil clock":   func(c *Config) { var clock *fixedClock; c.Clock = clock },
		"nul":               func(c *Config) { c.Directory = "a\x00b" },
	} {
		t.Run(name, func(t *testing.T) {
			c := testConfig(t)
			change(&c)
			if _, err := New(c); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}

func TestReadCompatibleLayoutAndOwnedBytes(t *testing.T) {
	config := testConfig(t)
	artifact(t, config, "historical", "admin", []byte(`{"version":3,"sourcesContent":["private"]}`), 0)
	store := newStore(t, config)
	got, err := store.Read(context.Background(), "historical", "admin/_assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if got.Release != "historical" || got.Asset != "admin/_assets/app.js" || !strings.Contains(string(got.Data), "private") {
		t.Fatalf("wrong result: %#v", got)
	}
	got.Data[0] = 'x'
	again, err := store.Read(context.Background(), "historical", "admin/_assets/app.js")
	if err != nil || again.Data[0] != '{' {
		t.Fatalf("borrowed map bytes: %v", err)
	}
	for _, test := range []struct {
		release telemetry.Release
		asset   Asset
	}{{"../bad", "admin/_assets/app.js"}, {"historical", "unknown/_assets/app.js"}, {"historical", "admin/_assets/%2f"}} {
		if _, err := store.Read(context.Background(), test.release, test.asset); err == nil {
			t.Fatal("accepted invalid read")
		}
	}
	if _, err := store.Read(context.Background(), "historical", "admin/_assets/missing.js"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing map: %v", err)
	}
}

func TestReadManifestValidation(t *testing.T) {
	for name, manifest := range map[string]string{
		"syntax":                   "{private contents",
		"release":                  `{"release":"wrong","portal":"admin","assets":{"_assets/app.js":"app.js.map"}}`,
		"portal":                   `{"release":"old","portal":"user","assets":{"_assets/app.js":"app.js.map"}}`,
		"traversal":                `{"release":"old","portal":"admin","assets":{"_assets/app.js":"../private"}}`,
		"unselected invalid entry": `{"release":"old","portal":"admin","assets":{"_assets/app.js":"app.js.map","../bad":"bad.map"}}`,
		"manifest as map":          `{"release":"old","portal":"admin","assets":{"_assets/app.js":"manifest.json"}}`,
		"null assets":              `{"release":"old","portal":"admin","assets":null}`,
		"large":                    strings.Repeat("x", manifestBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			config := testConfig(t)
			dir := artifact(t, config, "old", "admin", []byte("private"), 2*time.Hour)
			writeFile(t, filepath.Join(dir, "manifest.json"), []byte(manifest))
			store := newStore(t, config)
			_, err := store.Read(context.Background(), "old", "admin/_assets/app.js")
			if err == nil {
				t.Fatal("accepted invalid manifest")
			}
			for _, formatted := range []string{fmt.Sprint(err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
				if strings.Contains(formatted, config.Directory) || strings.Contains(formatted, "private contents") {
					t.Fatal("unsafe error diagnostic")
				}
			}
			if _, err := store.Prune(context.Background()); err == nil {
				t.Fatal("pruned malformed manifest")
			}
			if _, err := os.Stat(filepath.Join(dir, "app.js.map")); err != nil {
				t.Fatal("lost malformed release")
			}
		})
	}
}

func TestReadSizeAndRegularFile(t *testing.T) {
	for _, size := range []int{1024, 1025} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			c := testConfig(t)
			artifact(t, c, "old", "admin", make([]byte, size), 0)
			_, err := newStore(t, c).Read(context.Background(), "old", "admin/_assets/app.js")
			if (err == nil) != (size == 1024) {
				t.Fatalf("size %d: %v", size, err)
			}
		})
	}
	c := testConfig(t)
	dir := artifact(t, c, "old", "admin", nil, 0)
	name := filepath.Join(dir, "app.js.map")
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(name, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := newStore(t, c).Read(context.Background(), "old", "admin/_assets/app.js"); err == nil {
		t.Fatal("read directory")
	}
}

func TestSymlinksAreNotReadOrPruned(t *testing.T) {
	for _, level := range []string{"release", "portal", "manifest", "map"} {
		t.Run(level, func(t *testing.T) {
			c := testConfig(t)
			dir := artifact(t, c, "old", "admin", []byte("secret"), 2*time.Hour)
			name := filepath.Join(dir, "app.js.map")
			switch level {
			case "release":
				name = filepath.Join(c.Directory, "old")
			case "portal":
				name = dir
			case "manifest":
				name = filepath.Join(dir, "manifest.json")
			}
			target := name + "-target"
			if err := os.Rename(name, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, name); err != nil {
				t.Fatal(err)
			}
			store := newStore(t, c)
			if _, err := store.Read(context.Background(), "old", "admin/_assets/app.js"); err == nil {
				t.Fatal("read symlink")
			}
			_, _ = store.Prune(context.Background())
			if _, err := os.Stat(target); err != nil {
				t.Fatal("deleted symlink target")
			}
		})
	}
}

func TestMissingDisabledAndCancellation(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		c := testConfig(t)
		c.Directory = filepath.Join(c.Directory, "absent")
		if disabled {
			c.Directory = ""
		}
		store := newStore(t, c)
		if _, err := store.Read(context.Background(), "old", "admin/_assets/app.js"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read: %v", err)
		}
		if result, err := store.Prune(context.Background()); err != nil || result != (PruneResult{}) {
			t.Fatalf("prune: %+v %v", result, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := store.Read(ctx, "old", "admin/_assets/app.js"); !errors.Is(err, context.Canceled) {
			t.Fatalf("read cancellation: %v", err)
		}
		if _, err := store.Prune(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("prune cancellation: %v", err)
		}
	}
}

type panicClock struct{}

func (panicClock) Now() time.Time { panic("private clock detail") }

func TestConstructorDoesNotInvokeClock(t *testing.T) {
	c := testConfig(t)
	c.Clock = panicClock{}
	store := newStore(t, c)
	_, err := store.Prune(context.Background())
	if err == nil || strings.Contains(fmt.Sprintf("%+v", err), "private clock detail") {
		t.Fatalf("clock failure was not contained: %v", err)
	}
}

func TestManifestAssetInventoryBound(t *testing.T) {
	c := testConfig(t)
	dir := artifact(t, c, "old", "admin", nil, 0)
	assets := make(map[string]string, maxFiles)
	for i := 0; i < maxFiles; i++ {
		assets[fmt.Sprintf("_assets/a%d.js", i)] = "app.js.map"
	}
	data, err := json.Marshal(sourceMapManifest{Release: "old", Portal: "admin", Assets: assets})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "manifest.json"), data)
	if _, err := newStore(t, c).Read(context.Background(), "old", "admin/_assets/a0.js"); err == nil {
		t.Fatal("accepted excessive manifest assets")
	}
}
