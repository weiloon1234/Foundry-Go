package teamworkflow

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/secret"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	pgapp "github.com/weiloon1234/Foundry-Go/testkit/postgres/application"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type runningApp struct {
	app    *application.App
	client *httptest.Client
	stop   func()
}

func startApp(t testing.TB, main, receiver *pgtest.Scope, hooks Hooks, adjust ...func(*application.Settings)) *runningApp {
	t.Helper()
	settings := Defaults()
	environment, err := pgapp.Bind(settings.Services.Database, pgapp.On(main, "main"), pgapp.On(receiver, "receiver"))
	if err != nil {
		t.Fatal(err)
	}
	settings, err = settings.WithDatabaseScopes(environment.Settings())
	if err != nil {
		t.Fatal(err)
	}
	settings.Services.Namespace.Application = main.Schema()
	settings.Features.Idempotency.Config.DuplicateWait = 100 * time.Millisecond
	for _, change := range adjust {
		change(&settings)
	}
	app, err := Build(t.Context(), settings, hooks, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.Start(t, app, infrastructure.MigrationTarget{Definitions: Migrations()}, infrastructure.MigrationTarget{Connection: "receiver", Definitions: ReceiverMigrations()}); err != nil {
		t.Fatal(err)
	}
	return runApp(t, app)
}
func runApp(t testing.TB, app *application.App) *runningApp {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- app.Run(t.Context(), foundation.HTTP) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	address, err := app.HTTPReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result := &runningApp{app: app}
	var once sync.Once
	result.stop = func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := app.Shutdown(ctx); err != nil {
				t.Error(err)
			}
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Error(err)
				}
			case <-ctx.Done():
				t.Error("workflow HTTP kernel did not stop")
			}
		})
	}
	t.Cleanup(result.stop)
	result.client = httptest.Connect(t, "http://"+address)
	return result
}
func (a *runningApp) send(ctx context.Context, method, path, actor, key, media string, body []byte) (httpclient.Response, error) {
	request := a.client.Request(method, path)
	if actor != "" {
		request = request.Bearer(secret.New(actor))
	}
	if key != "" {
		request = request.Header("Idempotency-Key", key)
	}
	if media != "" {
		request = request.Header("Content-Type", media)
	}
	if body != nil {
		request = request.WithBody(httpclient.Bytes(body))
	}
	return a.client.Do(ctx, request)
}
func send(t testing.TB, a *runningApp, method, path, actor, key, body string, status int) httpclient.Response {
	t.Helper()
	response, err := a.send(t.Context(), method, path, actor, key, "application/json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.Status() != status {
		t.Fatalf("workflow status %d, wanted %d: %s", response.Status(), status, response.Bytes())
	}
	return response
}
func scalar(t testing.TB, db *database.DB, statement string, args ...any) int64 {
	t.Helper()
	var result int64
	if err := database.ScanOne(t.Context(), db, statement, args, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func waitFor(t testing.TB, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for !condition() {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("workflow condition did not become true")
		}
	}
}
