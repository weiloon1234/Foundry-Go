package configuredprofile

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"io"
	"log/slog"
	"testing"
	"time"
)

func quiet() application.Option {
	return application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func stop(t testing.TB, app *application.App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}
func built(t testing.TB) *application.App {
	t.Helper()
	app, err := Build(context.Background(), Defaults(), quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return app
}
func TestConfiguredProfileHTTP(t *testing.T) {
	app, err := Build(t.Context(), Defaults(), quiet())
	if err != nil {
		t.Fatal(err)
	}
	if err := Smoke(t.Context(), app); err != nil {
		t.Fatal(err)
	}
}
func TestConfiguredProfileNamesAndDefaults(t *testing.T) {
	app := built(t)
	resources := app.Resources()
	primary, err := resources.Cache()
	if err != nil {
		t.Fatal(err)
	}
	alias, err := resources.Caches.Store(Primary)
	if err != nil || primary != alias {
		t.Fatal("default lost alias identity", err)
	}
	reports, err := NamedCache(resources)
	if err != nil || reports == primary {
		t.Fatal("named cache is not independent", err)
	}
	values, err := Greetings.Bind(primary)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Greetings.Bind(reports)
	if err != nil {
		t.Fatal(err)
	}
	if err := values.Put(t.Context(), "message", "owned", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := other.Get(t.Context(), "message"); err != nil || hit {
		t.Fatal("named values leaked", err)
	}
	if _, err := resources.Caches.Store("missing"); err == nil {
		t.Fatal("unknown name fell back")
	}
}

func TestSmokePreservesFailureJoinedToCancellation(t *testing.T) {
	failed := errors.New("fixture cleanup failure")
	app, err := application.New(Defaults(), quiet()).HTTP(Routes).Register(foundation.Module{
		Name: "profile.failed-cleanup",
		OnBoot: func(_ context.Context, r *foundation.Runtime) error {
			return r.OnShutdown("fixture", func(context.Context) error { return errors.Join(context.Canceled, failed) })
		},
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := Smoke(t.Context(), app); !errors.Is(err, failed) {
		t.Fatal("expected cancellation hid cleanup failure", err)
	}
	select {
	case <-app.Done():
	default:
		t.Fatal("failed cleanup did not release application ownership")
	}
}
