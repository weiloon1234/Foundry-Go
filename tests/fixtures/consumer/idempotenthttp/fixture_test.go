package idempotenthttp

import (
	"bytes"
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
	stdhttp "net/http"
	"sync"
	"testing"
	"time"
)

type runningApp struct {
	app    *application.App
	url    string
	client *stdhttp.Client
	stop   func()
}

func startApp(t testing.TB, scope *pgtest.Scope, hooks Hooks, adjust func(*application.Settings)) *runningApp {
	t.Helper()
	settings := Defaults()
	environment, err := pgapp.Bind(settings.Services.Database, pgapp.On(scope, "default"))
	if err != nil {
		t.Fatal(err)
	}
	settings, err = settings.WithDatabaseScopes(environment.Settings())
	if err != nil {
		t.Fatal(err)
	}
	settings.Services.Namespace.Application = scope.Schema()
	settings.Features.Idempotency.Config.DuplicateWait = 150 * time.Millisecond
	if adjust != nil {
		adjust(&settings)
	}
	app, err := Build(t.Context(), settings, hooks, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.Start(t, app, infrastructure.MigrationTarget{Definitions: Migrations()}); err != nil {
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
	transport := stdhttp.DefaultTransport.(*stdhttp.Transport).Clone()
	transport.MaxConnsPerHost = 4
	result := &runningApp{app: app, url: "http://" + address, client: &stdhttp.Client{Transport: transport, Timeout: 10 * time.Second}}
	var once sync.Once
	result.stop = func() {
		once.Do(func() {
			transport.CloseIdleConnections()
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
				t.Error("HTTP kernel did not stop")
			}
		})
	}
	t.Cleanup(result.stop)
	return result
}

type wireResponse struct {
	status int
	body   []byte
	header stdhttp.Header
	err    error
}

func (a *runningApp) send(ctx context.Context, path, token, key, body string) wireResponse {
	request, err := stdhttp.NewRequestWithContext(ctx, "POST", a.url+path, bytes.NewBufferString(body))
	if err != nil {
		return wireResponse{err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return wireResponse{err: err}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return wireResponse{status: response.StatusCode, body: data, header: response.Header, err: err}
}
func assertResponse(t testing.TB, response wireResponse, status int) {
	t.Helper()
	if response.err != nil || response.status != status {
		t.Fatalf("HTTP status %d, wanted %d: %v %s", response.status, status, response.err, response.body)
	}
}
func scalar(t testing.TB, db *database.DB, statement string, args ...any) int64 {
	t.Helper()
	var result int64
	if err := database.ScanOne(t.Context(), db, statement, args, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
