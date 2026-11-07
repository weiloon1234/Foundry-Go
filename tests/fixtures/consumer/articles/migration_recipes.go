package articles

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
)

// doc-example:begin inspect-migrations
func MigrationStatus(ctx context.Context, db *database.DB, registry *migrate.Registry) (migrate.Report, error) {
	runner, err := migrate.NewPostgres(db, registry, migrate.DefaultPostgresConfig())
	if err != nil {
		return migrate.Report{}, err
	}
	return runner.Status(ctx)
}

// doc-example:end inspect-migrations

// doc-example:begin apply-migrations
func ApplyMigrations(ctx context.Context, runner *migrate.Postgres) (migrate.RunResult, error) {
	return runner.Up(ctx)
}

// doc-example:end apply-migrations
