package application_test

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/redis"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func nativeRedis(t *testing.T) infrastructure.RedisConnectionSettings {
	t.Helper()
	address := os.Getenv("FOUNDRY_TEST_REDIS_ADDR")
	if address == "" {
		if os.Getenv("FOUNDRY_TEST_REDIS_REQUIRED") == "1" {
			t.Fatal("required Redis endpoint missing")
		}
		t.Skip("native Redis is opt-in")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	c := infrastructure.DefaultRedisConnectionSettings()
	c.Host = host
	c.Port = uint16(number)
	c.TLS = redis.DisableTLS
	return c
}
func TestConfiguredOutboxKeepsTransactionSchemaAndNamedQueue(t *testing.T) {
	migrationDB := pgtest.Open(t)
	schema := pgtest.Namespace(t, migrationDB)
	s := settings()
	s.HTTP.Enabled = false
	s.Services.Namespace.Application = "outbox-" + rand.Text()
	databaseConfig := infrastructure.DefaultConnectionSettings()
	databaseConfig.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": databaseConfig, "other": databaseConfig}
	s.Services.Redis.Connections = infrastructure.RedisConnections{"default": nativeRedis(t)}
	connection := infrastructure.DefaultJobConnectionSettings()
	connection.Driver = infrastructure.RedisJobs
	connection.DefaultQueue = "configured"
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": connection, "second": connection}
	s.Worker.Enabled = true
	s.Worker.Config.PollInterval = time.Millisecond
	s.Features.Outbox.Enabled = true
	s.Features.Outbox.Schema = schema
	s.Features.Outbox.PollInterval = 5 * time.Millisecond
	s.Features.Outbox.Jobs = map[jobs.ConnectionName]outbox.Destination{"default": "primary", "second": "secondary"}
	definition := jobs.Define[assemblyPayload]("configured-outbox", 1, jobs.DefaultPolicy("unused"))
	received := make(chan string, 4)
	construct := func(application.Services) (jobs.Handler[assemblyPayload], error) {
		return func(_ context.Context, p assemblyPayload) error { received <- p.Value; return nil }, nil
	}
	app, err := application.New(s, quiet()).Jobs(application.Job(definition, construct), application.Job(definition, construct).On("second")).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	// An explicit migration task uses a separate prepared pool before startup.
	if err := migrationDB.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		for _, target := range app.Migrations() {
			for _, definition := range target.Definitions {
				for _, sql := range definition.SQL {
					if _, err := tx.Exec(t.Context(), sql); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.Worker) }()
	db, _ := app.Resources().Database()
	jobsConnection, _ := app.Resources().JobConnection()
	bound, _ := definition.On(jobsConnection)
	producer, err := app.Resources().JobOutbox("")
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback-fixture")
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := bound.Enqueue(t.Context(), tx, producer, assemblyPayload{"rolled-back"}, jobs.Options[assemblyPayload]{}); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	pending, err := bound.Capture(t.Context(), assemblyPayload{"committed"}, jobs.Options[assemblyPayload]{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		var before, after string
		if err := database.ScanOne(t.Context(), tx, "SHOW search_path", nil, &before); err != nil {
			return err
		}
		if _, err := pending.Enqueue(t.Context(), tx, producer); err != nil {
			return err
		}
		if err := database.ScanOne(t.Context(), tx, "SHOW search_path", nil, &after); err != nil {
			return err
		}
		if before != after {
			return errors.New("outbox changed business search_path")
		}
		record, err := bound.Inspect(t.Context(), pending.ID(), "")
		if err != nil {
			return err
		}
		if record.IsSet() {
			return errors.New("job published before commit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-received:
		if message != "committed" {
			t.Fatal("rolled back job published")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("committed outbox did not route to default queue")
	}
	other, _ := app.Resources().Databases.Connection("other")
	if err := other.Transaction(t.Context(), func(tx *database.Tx) error { _, err := pending.Enqueue(t.Context(), tx, producer); return err }); err == nil {
		t.Fatal("outbox accepted foreign pool")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("outbox worker did not drain")
	}
}
