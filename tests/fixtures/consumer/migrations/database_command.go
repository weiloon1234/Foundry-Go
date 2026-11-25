package migrations

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/weiloon1234/Foundry-Go/application"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

// RunDatabaseCommand declares the application's history next to the framework
// feature migrations and runs one database command without starting: only the
// selected connection's primary pool is opened.
// doc-example:begin application-migrations
func RunDatabaseCommand(ctx context.Context, settings application.Settings, args []string, stdout, stderr io.Writer) (err error) {
	invocation, err := dbcommand.Parse(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	app, err := application.New(settings).Migrations(infrastructure.MigrationTarget{Definitions: Definitions()}).Build(ctx)
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), app.ShutdownTimeout())
		defer cancel()
		err = errors.Join(err, app.Shutdown(shutdown))
	}()
	return app.RunDatabaseCommand(ctx, invocation, application.DatabaseCommandResources{}, stdout)
}

// doc-example:end application-migrations
