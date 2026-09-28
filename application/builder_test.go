package application_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/plugin"
	"github.com/weiloon1234/Foundry-Go/secret"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func settings() application.Settings {
	s := application.DefaultSettings()
	s.HTTP.Server.Address = "127.0.0.1:0"
	s.Services.Cache.Stores = infrastructure.CacheStores{"default": infrastructure.DefaultCacheSettings()}
	return s
}
func quiet() application.Option {
	return application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func plainRoutes(application.Services) ([]http.RouteRegistration, error) {
	return []http.RouteRegistration{http.DefineRoute(http.RouteSpec{ID: "fixture.ok", Method: http.GET, Access: http.Public}, http.StaticPath("/ok")).HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) { w.WriteHeader(204) })}, nil
}
func stop(t *testing.T, app *application.App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}
func TestConfiguredApplicationsOwnIndependentDefaults(t *testing.T) {
	config := settings()
	first, err := application.New(config, quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.New(config, quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, first); stop(t, second) })
	a, _ := first.Resources().Cache()
	b, _ := second.Resources().Cache()
	if a == b {
		t.Fatal("apps shared resources")
	}
	alias, _ := first.Resources().Caches.Store("default")
	if alias != a {
		t.Fatal("default is not an alias")
	}
	declaration := cache.Define("assembly", cache.StringKeys[string](), cache.JSON[string]())
	av, _ := declaration.Bind(a)
	bv, _ := declaration.Bind(b)
	if err := av.Put(t.Context(), "x", "one", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := bv.Get(t.Context(), "x"); err != nil || hit {
		t.Fatal("independent config leaked", err)
	}
}
func TestApplicationGlobalMiddlewareAndCompletion(t *testing.T) {
	var calls []string
	wrapper := func(id http.MiddlewareID) http.Middleware {
		return http.DefineMiddleware(id, func(next stdhttp.Handler) (stdhttp.Handler, error) {
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Add("X-Order", string(id))
				next.ServeHTTP(w, r)
			}), nil
		})
	}
	events := make(chan http.RequestEvent, 4)
	app, err := application.New(settings(), quiet()).HTTP(plainRoutes).Use(wrapper("first"), wrapper("second")).ObserveHTTP(http.RequestObserverFunc(func(_ context.Context, e http.RequestEvent) { events <- e })).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	t.Cleanup(func() { cancel(); stop(t, app) })
	address, err := app.HTTPReady(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/ok", "/missing"} {
		response, err := stdhttp.Get("http://" + address + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		calls = response.Header.Values("X-Order")
		if strings.Join(calls, ",") != "first,second" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("global policy missed response")
		}
		select {
		case e := <-events:
			if e.RequestID == "" {
				t.Fatal("missing attribution")
			}
		case <-time.After(time.Second):
			t.Fatal("completion was not delivered")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP kernel did not drain")
	}
}
func TestBuildAndFailedBootDoNotLeakOwnedLogFiles(t *testing.T) {
	s := settings()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	s.Log.Channels["default"] = logging.ChannelSettings{Sink: logging.SinkConfig{Driver: logging.File, Path: path}}
	app, err := application.New(s).HTTP(plainRoutes).Register(foundation.Module{Name: "fixture.fail", Requires: []foundation.ProviderID{application.Provider}, OnBoot: func(context.Context, *foundation.Runtime) error { return errors.New("fixture startup failure") }}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Build opened log file")
	}
	if err := app.Start(t.Context()); err == nil {
		t.Fatal("expected startup failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = app.Shutdown(ctx)
	<-app.Done()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app.Logger().Info("after-close")
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("closed app still wrote to owned sink")
	}
}
func TestBuilderSnapshotsAndRejectsInvalidConfigurationBeforeIO(t *testing.T) {
	s := settings()
	builder := application.New(s, quiet()).HTTP(plainRoutes)
	delete(s.Services.Cache.Stores, "default")
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if _, err := app.Resources().Cache(); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(t.Context()); err == nil {
		t.Fatal("builder reused")
	}
	bad := settings()
	bad.HTTP.Server.Address = "invalid"
	if _, err := application.New(bad, quiet()).Build(t.Context()); err == nil {
		t.Fatal("invalid listener accepted")
	}
	bad = settings()
	bad.HTTP.Enabled = false
	if _, err := application.New(bad, quiet()).HTTP(plainRoutes).Build(t.Context()); err == nil {
		t.Fatal("disabled HTTP discarded declarations")
	}
}

func TestBuilderDiagnosticFormattingPreservesSecrets(t *testing.T) {
	s := settings()
	db := infrastructure.DefaultConnectionSettings()
	db.Primary.Host = "localhost"
	db.Primary.Database = "fixture"
	db.Primary.User = "fixture"
	db.Primary.Password = secret.New("do-not-format-this")
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": db}
	builder := application.New(s, quiet())
	if strings.Contains(fmt.Sprintf("%+v %#v", builder, *builder), "do-not-format-this") {
		t.Fatal("builder formatting exposed private config")
	}
}

// Both builds and starts overlap, exercising per-application snapshots and
// provider ownership with different generated settings under the race detector.
func TestParallelApplicationsKeepDifferentConfiguration(t *testing.T) {
	type result struct {
		app   *application.App
		err   error
		index int
	}
	results := make(chan result, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for index := range 2 {
		go func() {
			s := settings()
			s.HTTP.Enabled = false
			s.Image.Enabled = index == 1
			name := cache.StoreName([]string{"first", "second"}[index])
			s.Services.Cache.Default = name
			s.Services.Cache.Stores = infrastructure.CacheStores{name: infrastructure.DefaultCacheSettings()}
			app, err := application.New(s, quiet()).Build(ctx)
			if err == nil {
				err = app.Start(ctx)
			}
			results <- result{app, err, index}
		}()
	}
	var apps [2]*application.App
	for range 2 {
		result := <-results
		if result.app != nil {
			apps[result.index] = result.app
			t.Cleanup(func() { stop(t, result.app) })
		}
		if result.err != nil {
			t.Error(result.err)
		}
	}
	if t.Failed() {
		return
	}
	first, err := apps[0].Resources().Cache()
	if err != nil {
		t.Fatal(err)
	}
	second, err := apps[1].Resources().Cache()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("parallel apps shared a store")
	}
	for index, name := range []cache.StoreName{"first", "second"} {
		selected, err := apps[index].Resources().Caches.Store(name)
		expected := []*cache.Store{first, second}[index]
		if err != nil || selected != expected {
			t.Fatal("default alias did not retain the configured name", err)
		}
		other := []cache.StoreName{"second", "first"}[index]
		if _, err := apps[index].Resources().Caches.Store(other); err == nil {
			t.Fatal("another app's configuration leaked")
		}
		_, err = apps[index].Resources().Image()
		if (index == 0 && err == nil) || (index == 1 && err != nil) {
			t.Fatal("image enablement was not application scoped", err)
		}
	}
	declaration := cache.Define("parallel", cache.StringKeys[string](), cache.JSON[string]())
	a, err := declaration.Bind(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := declaration.Bind(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Put(ctx, "shared-key", "first", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := b.Get(ctx, "shared-key"); err != nil || hit {
		t.Fatal("parallel app cache leaked", err)
	}
	stop(t, apps[0])
	if err := b.Put(ctx, "shared-key", "second", cache.Forever()); err != nil {
		t.Fatal("closing another app closed this store", err)
	}
	value, hit, err := b.Get(ctx, "shared-key")
	if err != nil || !hit || value != "second" {
		t.Fatal("surviving app lost its cache", err)
	}
}

func TestApplicationCombinesOrdinaryAndPluginRoutes(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			id, path := http.RouteID("plugin.extra"), "/plugin"
			if collision {
				id, path = "fixture.ok", "/ok"
			}
			route := http.DefineRoute(http.RouteSpec{ID: id, Method: http.GET, Access: http.Public}, http.StaticPath(path))
			extension := plugin.Module{Declaration: plugin.Manifest{ID: "fixture.routes", Version: "1.0.0", Framework: "*"}, OnRegister: func(r *plugin.Registrar) error {
				if err := http.RegisterRoute(r, application.RouterKey, route, func(foundation.Resolver) (http.RouteRegistration, error) {
					return route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ http.NoPath) { w.WriteHeader(204) }), nil
				}); err != nil {
					return err
				}
				return http.RegisterMiddleware(r, application.RouterKey, http.DefineMiddleware("plugin.header", func(next stdhttp.Handler) (stdhttp.Handler, error) {
					return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
						w.Header().Set("X-Plugin", "present")
						next.ServeHTTP(w, r)
					}), nil
				}))
			}}
			app, err := application.New(settings(), quiet()).HTTP(plainRoutes).RegisterPlugin(extension).Build(t.Context())
			if collision {
				if !errors.Is(err, fault.Duplicate) {
					t.Fatalf("ordinary/plugin route collision was not rejected: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stop(t, app) })
			router, err := foundation.Resolve(app.Services(), application.RouterKey)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/ok", "/plugin"} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
				if response.Code != 204 || response.Header().Get("X-Plugin") != "present" {
					t.Fatalf("route %s missed merged middleware: %d", path, response.Code)
				}
			}
		})
	}
}
