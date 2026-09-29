package infrastructure_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
)

var values = cache.Define("configured-values", cache.StringKeys[string](), cache.JSON[string]())

func memorySettings() infrastructure.Settings {
	s := infrastructure.DefaultSettings()
	s.Cache.Stores = infrastructure.CacheStores{"default": infrastructure.DefaultCacheSettings(), "second": infrastructure.DefaultCacheSettings()}
	return s
}
func built(t *testing.T, p *infrastructure.Plan, extra ...foundation.Provider) *foundation.App {
	t.Helper()
	app, err := p.Register(foundation.NewBuilder()).Register(extra...).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return app
}
func services(t *testing.T, app *foundation.App) *infrastructure.Services {
	t.Helper()
	s, err := foundation.Resolve(app.Services(), infrastructure.ServicesKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestDefaultAliasesIndependentApplicationsAndCleanup(t *testing.T) {
	settings := memorySettings()
	plan, err := infrastructure.Configure(settings)
	if err != nil {
		t.Fatal(err)
	}
	settings.Cache.Stores["default"] = infrastructure.CacheSettings{} // Plan owns its snapshot.
	var onShutdown bool
	domain := foundation.Module{Name: "domain", Requires: []foundation.ProviderID{infrastructure.Provider}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		s, err := foundation.Resolve(r.Services(), infrastructure.ServicesKey)
		if err != nil {
			return err
		}
		store, err := s.Cache()
		if err != nil {
			return err
		}
		bound, err := values.Bind(store)
		if err != nil {
			return err
		}
		return r.OnShutdown("flush", func(ctx context.Context) error {
			onShutdown = true
			return bound.Put(ctx, "shutdown", "ok", cache.Forever())
		})
	}}
	first, second := built(t, plan, domain), built(t, plan)
	for _, app := range []*foundation.App{first, second} {
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	firstServices, secondServices := services(t, first), services(t, second)
	a, _ := firstServices.Cache()
	explicit, _ := firstServices.Caches.Store("default")
	other, _ := firstServices.Caches.Store("second")
	b, _ := secondServices.Cache()
	if a != explicit || a == other || a == b {
		t.Fatal("default identity or per-application ownership changed")
	}
	if _, err := firstServices.Caches.Store("missing"); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
	va, _ := values.Bind(a)
	vb, _ := values.Bind(b)
	vo, _ := values.Bind(other)
	if err := va.Put(t.Context(), "one", "first", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	for _, v := range []cache.Cache[string, string]{vb, vo} {
		if _, hit, err := v.Get(t.Context(), "one"); err != nil || hit {
			t.Fatal("cache isolation failed", err)
		}
	}
	if err := first.Shutdown(context.Background()); err != nil || !onShutdown {
		t.Fatal("borrower cleanup failed", err)
	}
	if _, _, err := va.Get(t.Context(), "one"); !errors.Is(err, fault.Closed) {
		t.Fatal("owned memory cache stayed open", err)
	}
	if err := vb.Put(t.Context(), "still", "active", cache.Forever()); err != nil {
		t.Fatal(err)
	}
}
func TestPureBuildAndFailedBootClosesEarlierResources(t *testing.T) {
	s := memorySettings()
	root := t.TempDir()
	disk := infrastructure.DefaultDiskSettings()
	disk.Local.Root = root
	s.Storage.Disks = infrastructure.Disks{"default": disk}
	p, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	app := built(t, p)
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("build performed filesystem IO")
	}
	registry := services(t, app).Storage
	a, _ := registry.Default()
	b, _ := registry.Disk("default")
	if a != b {
		t.Fatal("default disk duplicated")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(root)
	if len(entries) == 0 {
		t.Fatal("storage was not booted")
	}
	broken := memorySettings()
	c := infrastructure.DefaultCacheSettings()
	c.Driver = infrastructure.FileCache
	c.File.Root = filepath.Join(t.TempDir(), "missing")
	broken.Cache.Stores["z-fails"] = c
	p, err = infrastructure.Configure(broken)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := p.Register(foundation.NewBuilder()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = failed.Shutdown(context.Background()) })
	store, _ := services(t, failed).Cache()
	bound, _ := values.Bind(store)
	if err = failed.Start(t.Context()); err == nil {
		t.Fatal("missing cache root booted")
	}
	// Failed startup returns its cause again at Shutdown; wait directly for full cleanup.
	select {
	case <-failed.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("failed startup did not drain")
	}
	if _, _, err := bound.Get(t.Context(), "one"); !errors.Is(err, fault.Closed) {
		t.Fatal("partial startup leaked memory backend", err)
	}
}
func TestConfigurationRejectsInvalidGraphBeforeIO(t *testing.T) {
	cases := []func(*infrastructure.Settings){
		func(s *infrastructure.Settings) { s.Cache.Default = "missing" },
		func(s *infrastructure.Settings) {
			c := s.Cache.Stores["default"]
			c.Driver = "unknown"
			s.Cache.Stores["default"] = c
		},
		func(s *infrastructure.Settings) {
			c := s.Cache.Stores["default"]
			c.Driver = infrastructure.RedisCache
			s.Cache.Stores["default"] = c
		},
		func(s *infrastructure.Settings) {
			c := s.Cache.Stores["default"]
			c.Driver = infrastructure.PostgresCache
			s.Cache.Stores["default"] = c
		},
		func(s *infrastructure.Settings) {
			c := s.Cache.Stores["default"]
			c.Require.DistributedFills = true
			s.Cache.Stores["default"] = c
		},
		func(s *infrastructure.Settings) {
			c := s.Cache.Stores["default"]
			c.Driver = infrastructure.FileCache
			c.File.Root = t.TempDir()
			c.Require.Tags = true
			s.Cache.Stores["default"] = c
		},
		func(s *infrastructure.Settings) {
			c := s.Cache.Stores["default"]
			c.Driver = infrastructure.FileCache
			c.File.Root = t.TempDir()
			c.File.PruneInterval = time.Millisecond
			s.Cache.Stores["default"] = c
		},
		func(s *infrastructure.Settings) {
			c := infrastructure.DefaultConnectionSettings()
			c.Primary.Host = "localhost"
			c.Primary.Database = "test"
			c.Primary.User = "test"
			s.Database.MaxConnections = 1
			s.Database.Connections = infrastructure.DatabaseConnections{"default": c}
		},
		func(s *infrastructure.Settings) {
			c := infrastructure.DefaultRedisConnectionSettings()
			c.Host = "localhost"
			s.Redis.MaxConnections = 1
			s.Redis.Connections = infrastructure.RedisConnections{"default": c}
		},
	}
	for i, change := range cases {
		s := memorySettings()
		change(&s)
		if _, err := infrastructure.Configure(s); err == nil {
			t.Fatalf("invalid graph %d accepted", i)
		}
	}
}
func TestNamedTOMLTablesUseGeneratedNestedCodecs(t *testing.T) {
	schema, err := infrastructure.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	document := `[database.connections.default.primary]
host='localhost'
user='test'
database='test'
password='private-value'
[database.connections.default.primary.pool]
connect_timeout='7s'
[redis.connections.default]
host='localhost'
operation_timeout='8s'
[cache.stores.default]
driver='memory'
[cache.stores.second]
driver='memory'
[cache.stores.second.config]
timeout='2s'
`
	layer, err := toml.Decode(strings.NewReader(document), schema, toml.Options{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, report, err := schema.Load(infrastructure.DefaultSettings(), config.Inputs[infrastructure.Settings]{Files: []config.Values{layer}, Validate: infrastructure.Settings.Validate})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Database.Connections[database.ConnectionName("default")].Primary.Pool.ConnectTimeout != 7*time.Second || loaded.Redis.Connections[redis.ConnectionName("default")].OperationTimeout != 8*time.Second || loaded.Cache.Stores["second"].Config.Timeout != 2*time.Second {
		t.Fatal("duration types lost")
	}
	sensitive := false
	for _, entry := range report.Entries() {
		if entry.Name == "database.connections" {
			sensitive = entry.Secret
		}
	}
	if !sensitive {
		t.Fatal("connection provenance is not sensitive")
	}
	// A typed disk name cannot select a database/Redis/cache instance (compiler fixtures).
	var _ storage.DiskID = "default"
}

func TestPlanFormattingPreservesSecretBoundary(t *testing.T) {
	s := memorySettings()
	c := infrastructure.DefaultConnectionSettings()
	c.Primary.Host = "localhost"
	c.Primary.Database = "test"
	c.Primary.User = "test"
	c.Primary.Password = secret.New("plan-secret-value")
	s.Database.Connections = infrastructure.DatabaseConnections{"default": c}
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", plan, plan), "plan-secret-value") {
		t.Fatal("private plan configuration leaked")
	}
}
