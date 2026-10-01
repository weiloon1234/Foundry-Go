package consumer_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/migrations"
	"github.com/weiloon1234/Foundry-Go/application"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

// The consumer's binary parses before assembling its services. A future CLI
// kernel will host the same feature commands; this is not a starter entrypoint.
func Example_databaseCommand() {
	run := func(ctx context.Context, args []string, resources dbcommand.Resources, stdout, stderr io.Writer) error {
		invocation, err := dbcommand.Parse(args, stderr)
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if err != nil {
			return err
		}
		return invocation.Run(ctx, resources, stdout)
	}
	_ = run
}

// A configured application declares its own history next to the framework
// feature migrations and runs one database command without starting: only the
// selected connection's primary pool is opened.
func Example_applicationDatabaseCommand() {
	run := func(ctx context.Context, settings application.Settings, args []string, stdout, stderr io.Writer) error {
		invocation, err := dbcommand.Parse(args, stderr)
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if err != nil {
			return err
		}
		app, err := application.New(settings).Migrations(infrastructure.MigrationTarget{Definitions: migrations.Definitions()}).Build(ctx)
		if err != nil {
			return err
		}
		defer func() {
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), app.ShutdownTimeout())
			defer cancel()
			_ = app.Shutdown(shutdown)
		}()
		return app.RunDatabaseCommand(ctx, invocation, application.DatabaseCommandResources{}, stdout)
	}
	_ = run
}

func TestDatabaseCommandHelpWithoutBootstrap(t *testing.T) {
	var help bytes.Buffer
	if _, err := dbcommand.Parse([]string{"migrate", "status", "--help"}, &help); !errors.Is(err, flag.ErrHelp) || help.Len() == 0 {
		t.Fatalf("consumer help: %q %v", help.String(), err)
	}
}

func TestApplicationDatabaseCommandRefusesUnknownDatabaseBeforeIO(t *testing.T) {
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.Services.Cache.Stores = infrastructure.CacheStores{"default": infrastructure.DefaultCacheSettings()}
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary.Host, connection.Primary.Database, connection.Primary.User = "unreachable.invalid", "consumer", "consumer"
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	app, err := application.New(s).Migrations(infrastructure.MigrationTarget{Definitions: migrations.Definitions()}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if targets := app.Migrations(); len(targets) != 1 || len(targets[0].Definitions) != 2 {
		t.Fatalf("application migration targets: %+v", targets)
	}
	// Showing a definition reads the registry only, so the unreachable pool never connects.
	show, err := dbcommand.Parse([]string{"migrate", "show", "--migration", string(migrations.Origin) + "/" + string(migrations.AddRecordLabel)}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := app.RunDatabaseCommand(t.Context(), show, application.DatabaseCommandResources{}, &output); err != nil || !strings.Contains(output.String(), "ALTER TABLE consumer_records ADD COLUMN label") {
		t.Fatalf("migration definition: %q %v", output.String(), err)
	}
	invocation, err := dbcommand.Parse([]string{"migrate", "status", "--database", "reports"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RunDatabaseCommand(t.Context(), invocation, application.DatabaseCommandResources{}, io.Discard); !errors.Is(err, fault.Missing) {
		t.Fatal("unknown database selection accepted", err)
	}
}
