package tooling_test

import (
	"context"
	"testing"

	"foundry.test/consumer/configuredprofile"
	"foundry.test/consumer/isolatedhttp"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	appstest "github.com/weiloon1234/Foundry-Go/testkit/application"
	attachmentstest "github.com/weiloon1234/Foundry-Go/testkit/attachments"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	pgapp "github.com/weiloon1234/Foundry-Go/testkit/postgres/application"
	redistest "github.com/weiloon1234/Foundry-Go/testkit/redis"
)

// doc-example:begin run-http-test
func TestConfiguredHTTPKernel(t *testing.T) {
	app, err := configuredprofile.Build(t.Context(), configuredprofile.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	baseURL := appstest.RunHTTP(t, app)
	client := httptest.Connect(t, baseURL)
	response, err := client.Do(t.Context(), client.Get("/profile"))
	if err != nil {
		t.Fatal(err)
	}
	httptest.AssertStatus(t, response, 200)
}

// doc-example:end run-http-test

// doc-example:begin run-worker-test
func runWorker(t testing.TB, app *application.App) {
	t.Helper()
	stop := appstest.RunKernel(t, app, foundation.Worker)
	// Exercise this application's registered jobs before stopping it.
	stop()
}

// doc-example:end run-worker-test

// doc-example:begin isolated-application
func isolatedApplication(t testing.TB) *application.App {
	t.Helper()
	scope, archive := pgtest.Isolate(t), pgtest.Isolate(t)
	settings := isolatedhttp.Defaults()
	env, err := pgapp.Bind(settings.Services.Database,
		pgapp.On(scope, "main", "reporting").WithReads(),
		pgapp.On(archive, "archive"),
	)
	if err != nil {
		t.Fatal(err)
	}
	settings, err = settings.WithDatabaseScopes(env.Settings())
	if err != nil {
		t.Fatal(err)
	}
	settings.Services.Namespace.Environment = scope.Schema()
	app, err := isolatedhttp.Build(t.Context(), settings, func(context.Context, isolatedhttp.Receipt) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Start(t, app,
		infrastructure.MigrationTarget{Definitions: isolatedhttp.Migrations()},
		infrastructure.MigrationTarget{Connection: "archive", Definitions: isolatedhttp.Migrations()},
	); err != nil {
		t.Fatal(err)
	}
	return app
}

// doc-example:end isolated-application

// doc-example:begin redis-test-settings
func redisSettings(t testing.TB) infrastructure.RedisSettings {
	t.Helper()
	connection := infrastructure.RedisSettingsFromConfig(redistest.Config(t))
	settings := infrastructure.DefaultRedisSettings()
	settings.Connections = infrastructure.RedisConnections{"default": connection}
	return settings
}

// doc-example:end redis-test-settings

// doc-example:begin redis-test-cleanup
func prepareRedisCleanup(t testing.TB, builder *application.Builder) {
	t.Helper()
	inventory, err := builder.RedisKeys()
	if err != nil {
		t.Fatal(err)
	}
	redistest.Cleanup(t, inventory)
}

// doc-example:end redis-test-cleanup

// doc-example:begin missing-attachment-object
func removeOriginal(t testing.TB, manager *attachments.Manager, file attachments.File[models.User]) {
	t.Helper()
	attachmentstest.RemoveBackingObject(t, manager, file)
}

// doc-example:end missing-attachment-object
