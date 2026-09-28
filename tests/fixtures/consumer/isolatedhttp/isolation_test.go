package isolatedhttp

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	pgapp "github.com/weiloon1234/Foundry-Go/testkit/postgres/application"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsolatedCompleteHTTPApplications(t *testing.T) {
	for _, text := range []string{"alpha", "beta"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			started := time.Now()
			scope, archive := pgtest.Isolate(t), pgtest.Isolate(t)
			original := Defaults()
			environment, err := pgapp.Bind(original.Services.Database, pgapp.On(scope, "main", "reporting").WithReads(), pgapp.On(archive, "archive"))
			if err != nil {
				t.Fatal(err)
			}
			settings, err := original.WithDatabaseScopes(environment.Settings())
			if err != nil {
				t.Fatal(err)
			}
			settings.Services.Namespace.Application = scope.Schema()
			var callbacks atomic.Int32
			var app *application.App
			app, err = Build(t.Context(), settings, func(ctx context.Context, receipt Receipt) error {
				db, err := app.Resources().Database()
				if err != nil {
					return err
				}
				found, err := QueryScopeRecords().Where(RecordFields().Name.Eq(receipt.Name)).RequireFirst(ctx, db)
				if err != nil {
					return err
				}
				if found.Text != text {
					return errors.New("callback did not observe real commit")
				}
				callbacks.Add(1)
				return nil
			}, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
			if err != nil {
				t.Fatal(err)
			}
			targets := []infrastructure.MigrationTarget{{Definitions: Migrations()}, {Connection: "archive", Definitions: Migrations()}}
			if err := environment.Start(t, app, targets...); err != nil {
				t.Fatal(err)
			}
			t.Logf("Full isolated application setup: %s", time.Since(started))
			db, _ := app.Resources().Database()
			named, _ := app.Resources().Databases.Connection("main")
			if named != db {
				t.Fatal("default did not alias named database")
			}
			reporting, _ := app.Resources().Databases.Connection("reporting")
			separate, _ := app.Resources().Databases.Connection("archive")
			if reporting == db || !db.HasReadPool() || separate.HasReadPool() {
				t.Fatal("scope sharing changed pool or read routing ownership")
			}
			records, err := Records(text)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := records.Create(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if count, err := QueryScopeRecords().Count(t.Context(), reporting); err != nil || count != 1 {
				t.Fatal("shared scope lost fixture", err)
			}
			if count, err := QueryScopeRecords().Count(t.Context(), separate); err != nil || count != 0 {
				t.Fatal("distinct scope leaked fixture", err)
			}
			finished := make(chan error, 1)
			go func() { finished <- app.Run(t.Context(), foundation.HTTP) }()
			ready, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			address, err := app.HTTPReady(ready)
			if err != nil {
				t.Fatal(err)
			}
			client := httptest.Connect(t, "http://"+address)
			send := func(input Submission, status int) {
				t.Helper()
				request, err := httptest.JSON(t.Context(), client.Post("/records"), SubmissionJSON(), input)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Do(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				httptest.AssertStatus(t, response, status)
				if status == 201 {
					receipt, err := httptest.DecodeJSON(t.Context(), response, ReceiptJSON())
					if err != nil || receipt.Text != text {
						t.Fatal("typed HTTP response mismatch", err)
					}
				}
			}
			send(Submission{Name: "shared", Text: text}, 201)
			send(Submission{Name: "rolled-back", Text: text, Rollback: true}, 500)
			if callbacks.Load() != 1 {
				t.Fatal("callbacks ran before commit or after rollback")
			}
			for _, name := range []string{"shared", "fixture"} {
				response, err := client.Do(t.Context(), client.Get("/records/"+name))
				if err != nil {
					t.Fatal(err)
				}
				httptest.AssertStatus(t, response, 200)
				receipt, err := httptest.DecodeJSON(t.Context(), response, ReceiptJSON())
				if err != nil || receipt.Text != text {
					t.Fatal("parallel app read another scope", err)
				}
			}
			if found, err := QueryScopeRecords().Where(RecordFields().Name.Eq("rolled-back")).First(t.Context(), db); err != nil || found.IsSet() {
				t.Fatal("HTTP rollback persisted", err)
			}
			var count int64
			if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM foundry_outbox", nil, &count); err != nil || count != 1 {
				t.Fatal("outbox did not share real commit/rollback", err)
			}
			// The configured app owns its publisher task. Await durable acceptance
			// rather than racing it with an extra manual publisher invocation.
			delivery, cancelDelivery := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelDelivery()
			for {
				err := database.ScanOne(delivery, db, "SELECT count(*) FROM scope_deliveries", nil, &count)
				if err != nil {
					t.Fatal(err)
				}
				if count == 1 {
					var state string
					if err := database.ScanOne(delivery, db, "SELECT publish_state FROM foundry_outbox", nil, &state); err != nil {
						t.Fatal(err)
					}
					if state == "published" {
						break
					}
				}
				select {
				case <-time.After(10 * time.Millisecond):
				case <-delivery.Done():
					t.Fatal("committed outbox was not published")
				}
			}
			if db.Stats().Open > 4 || reporting.Stats().Open > 4 || separate.Stats().Open > 2 {
				t.Fatal("scoped pools exceeded bounds")
			}
			t.Logf("Application pool connections: main=%d reporting=%d archive=%d", db.Stats().Open, reporting.Stats().Open, separate.Stats().Open)
			shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelShutdown()
			if err := app.Shutdown(shutdown); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-shutdown.Done():
				t.Fatal("HTTP kernel did not stop")
			}
			if db.Stats().Open != 0 || reporting.Stats().Open != 0 || separate.Stats().Open != 0 {
				t.Fatal("application teardown retained connections")
			}
			retained := scope.Open(t)
			if count, err := QueryScopeRecords().Count(t.Context(), retained); err != nil || count != 2 {
				t.Fatal("teardown lost retained data", err)
			}
		})
	}
}
