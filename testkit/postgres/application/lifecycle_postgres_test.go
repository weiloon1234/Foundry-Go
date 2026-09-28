package application_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	pgapp "github.com/weiloon1234/Foundry-Go/testkit/postgres/application"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestScopedApplicationFeatureMigrationsAndStartupFailure(t *testing.T) {
	scope := pgtest.Isolate(t)
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": infrastructure.DefaultConnectionSettings()}
	s.Features.Auth.Sessions.Enabled, s.Features.Auth.Tokens.Enabled, s.Features.Audit.Enabled = true, true, true
	s.Features.Extensions.Enabled, s.Features.Notifications.Enabled, s.Features.Reports.Enabled = true, true, true
	cache := infrastructure.DefaultCacheSettings()
	cache.Driver = infrastructure.PostgresCache
	s.Services.Cache.Stores = infrastructure.CacheStores{"default": cache}
	environment, err := pgapp.Bind(s.Services.Database, pgapp.On(scope, "default"))
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.WithDatabaseScopes(environment.Settings())
	if err != nil {
		t.Fatal(err)
	}
	quiet := application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	app, err := application.New(s, quiet).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Migrations()) != 1 || app.Migrations()[0].Schema != scope.Schema() {
		t.Fatal("feature migration targets were not shared")
	}
	if err := environment.Start(t, app); err != nil {
		t.Fatal(err)
	}
	db, _ := app.Resources().Database()
	var count int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM schema_migrations", nil, &count); err != nil || count < 6 {
		t.Fatal("persistent features were not migrated", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("intentional startup cancellation")
	failing, err := application.New(s, quiet).Register(foundation.Module{Name: "test.failure", Requires: []foundation.ProviderID{infrastructure.DatabaseProvider("default")}, OnBoot: func(context.Context, *foundation.Runtime) error { return failure }}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.Start(t, failing); !errors.Is(err, failure) {
		t.Fatal("startup failure not preserved", err)
	}
	failedDB, _ := failing.Resources().Database()
	if failedDB.Stats().Open != 0 || failedDB.Stats().Owners != 0 {
		t.Fatal("failed startup retained pooled resources")
	}
	retained := scope.Open(t)
	if err := database.ScanOne(t.Context(), retained, "SELECT count(*) FROM schema_migrations", nil, &count); err != nil || count < 6 {
		t.Fatal("startup failure deleted retained history", err)
	}
}
