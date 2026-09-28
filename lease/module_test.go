package lease_test

import (
	"context"
	"errors"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
)

func TestModuleDrainsBeforeBorrowedAdapter(t *testing.T) {
	backendKey := foundation.NewKey[*memory.Backend]("test.backend")
	managerKey := foundation.NewKey[*lease.Manager]("test.leases")
	closed := false
	adapter := foundation.Module{Name: "test.backend", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, backendKey, func(foundation.Resolver) (*memory.Backend, error) { return memory.New(8) })
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		b, err := foundation.Resolve(r.Services(), backendKey)
		if err != nil {
			return err
		}
		return r.OnShutdown("backend", func(context.Context) error {
			m, err := foundation.Resolve(r.Services(), managerKey)
			if err != nil {
				return err
			}
			select {
			case <-m.Done():
			default:
				return errors.New("backend closed before leases drained")
			}
			closed = true
			return b.Close()
		})
	}}
	module := lease.Module("test.leases", managerKey, lease.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "module"}), []foundation.ProviderID{adapter.Name}, func(r foundation.Resolver) (lease.Backend, error) { return foundation.Resolve(r, backendKey) })
	app, err := foundry.New().Register(module, adapter).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown(context.Background()) })
	m, err := foundation.Resolve(app.Services(), managerKey)
	if err != nil {
		t.Fatal(err)
	}
	if m.Stats().Active != 0 {
		t.Fatal("construction started work")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	l, _ := family.Bind(m)
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := l.With(t.Context(), "a", time.Second, 0, func(ctx context.Context) error { close(entered); <-ctx.Done(); return nil })
		done <- err
	}()
	<-entered
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !closed || m.Stats().Active != 0 {
		t.Fatal("lifecycle did not finish")
	}
}
func TestModuleCleansUpAfterLaterBootFailure(t *testing.T) {
	key := foundation.NewKey[*lease.Manager]("test.leases")
	b, _ := memory.New(2)
	t.Cleanup(func() { b.Close() })
	module := lease.Module("test.leases", key, lease.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "failed-boot"}), nil, func(foundation.Resolver) (lease.Backend, error) { return b, nil })
	failure := errors.New("later boot failed")
	later := foundation.Module{Name: "later", Requires: []foundation.ProviderID{module.Name}, OnBoot: func(context.Context, *foundation.Runtime) error { return failure }}
	app, err := foundry.New().Register(later, module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := app.Shutdown(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	select {
	case <-m.Done():
	default:
		t.Fatal("failed boot left manager open")
	}
}
