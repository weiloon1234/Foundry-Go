package datatable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestModuleRetainsBorrowedDependencyUntilQueriesAndArtifactsExit(t *testing.T) {
	key := foundation.NewKey[*Manager]("test.reports")
	closed := make(chan struct{})
	adapter := foundation.Module{Name: "test.database", OnBoot: func(_ context.Context, runtime *foundation.Runtime) error {
		return runtime.OnShutdown("borrowed-database", func(context.Context) error {
			manager, err := foundation.Resolve(runtime.Services(), key)
			if err != nil {
				return err
			}
			select {
			case <-manager.DoneQueries():
			default:
				return errors.New("database closed before query exit")
			}
			select {
			case <-manager.DoneExports():
			default:
				return errors.New("database closed before artifact exit")
			}
			close(closed)
			return nil
		})
	}}
	config := DefaultConfig()
	config.TempDir = t.TempDir()
	module := Module("test.reports", key, []foundation.ProviderID{adapter.Name}, func(foundation.Resolver) (*Manager, error) {
		return New(Dependencies{Database: new(database.DB)}, config)
	})
	app, err := foundation.NewBuilder().Register(module, adapter).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	manager, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	artifact := openedArtifact(t, manager, t.Context())
	entered, release := make(chan struct{}), make(chan struct{})
	queryCanceled := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	queryDone := make(chan error, 1)
	go func() {
		queryDone <- manager.calls.Run(t.Context(), "held query", func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			close(queryCanceled)
			<-release
			return ctx.Err()
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not begin")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := app.Shutdown(ctx); err == nil {
		t.Fatal("shutdown abandoned owned resources")
	}
	select {
	case <-queryCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel the query")
	}
	select {
	case <-closed:
		t.Fatal("borrowed dependency closed before actual exit")
	default:
	}
	if err := artifact.Close(); err != nil {
		t.Fatal(err)
	}
	finish()
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-queryDone; !errors.Is(err, context.Canceled) {
		t.Fatal("query did not observe shutdown", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("borrowed dependency did not close after exit")
	}
}

func TestModuleClosesManagerWhenLaterBootFails(t *testing.T) {
	key := foundation.NewKey[*Manager]("test.reports")
	module := Module("test.reports", key, nil, func(foundation.Resolver) (*Manager, error) {
		return New(Dependencies{Database: new(database.DB)}, DefaultConfig())
	})
	failure := errors.New("later module failed")
	later := foundation.Module{Name: "test.later", Requires: []foundation.ProviderID{module.Name}, OnBoot: func(context.Context, *foundation.Runtime) error { return failure }}
	app, err := foundation.NewBuilder().Register(later, module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := foundation.Resolve(app.Services(), key)
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
	case <-manager.DoneQueries():
	default:
		t.Fatal("failed boot retained query admission")
	}
	select {
	case <-manager.DoneExports():
	default:
		t.Fatal("failed boot retained export admission")
	}
}
