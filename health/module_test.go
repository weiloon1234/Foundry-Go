package health_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
)

func TestReadinessModuleDrainsBeforeItsDependency(t *testing.T) {
	key := foundation.NewKey[*health.Registry]("test.readiness")
	var dependencyClosed atomic.Bool
	release := make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	config := health.DefaultConfig()
	config.ProbeTimeout = 20 * time.Millisecond
	app, err := foundation.NewBuilder(foundation.WithShutdownTimeout(time.Second)).Register(
		foundation.Module{Name: "dependency", OnBoot: func(_ context.Context, runtime *foundation.Runtime) error {
			return runtime.OnShutdown("connection", func(context.Context) error { dependencyClosed.Store(true); return nil })
		}},
		health.Module("health", key, config, []foundation.ProviderID{"dependency"}, func(foundation.Resolver) ([]health.Probe, error) {
			return []health.Probe{{ID: "dependency", Check: func(context.Context) error {
				<-release
				if dependencyClosed.Load() {
					return errors.New("dependency closed during probe")
				}
				return nil
			}}}, nil
		}),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		resume()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	if report, err := registry.Check(t.Context()); err != nil || report.Ready {
		t.Fatal("blocked dependency reported ready", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	err = app.Shutdown(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || dependencyClosed.Load() {
		t.Fatal("shutdown discarded a live probe owner", err)
	}
	resume()
	ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if !dependencyClosed.Load() {
		t.Fatal("drained readiness did not release dependency shutdown")
	}
}
