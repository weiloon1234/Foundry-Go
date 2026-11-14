package app

import (
	"context"
	"fmt"
	"time"

	fixtureconfig "foundry.test/deployacceptance/internal/config"
	"foundry.test/deployacceptance/internal/work"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

var First = jobs.Define[work.Payload]("acceptance.first", 1, jobs.DefaultPolicy("first"))
var Second = jobs.Define[work.Payload]("acceptance.second", 1, jobs.DefaultPolicy("second"))

func Builder(settings fixtureconfig.Settings, receipts *work.Receipts) *application.Builder {
	// The compiled fixture owns this schema even when upload routes are disabled.
	settings.App.Features.Extensions.Enabled = true
	construct := func(application.Services) (jobs.Handler[work.Payload], error) { return receipts.Handle, nil }
	builder := application.New(settings.App).
		HTTP(healthRoutes).
		Jobs(application.Job(First, construct), application.Job(Second, construct)).
		Migrations(infrastructure.MigrationTarget{Connection: "primary", Schema: "public", Definitions: []migrate.Definition{{
			Key: migrate.Key{Origin: "deploy-acceptance", ID: "000001_fixture"}, Version: "v1",
			SQL: []string{`CREATE TABLE acceptance_fixture (name text PRIMARY KEY, value text NOT NULL)`},
		}}}, infrastructure.MigrationTarget{Connection: "primary", Schema: "public", Definitions: attachments.Migrations()}).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Owners: []extensions.Declaration{uploadIdentity.Registration()}, Readiness: []health.Probe{{ID: "acceptance.readiness", Check: func(ctx context.Context) error {
			return readiness(ctx, settings.Fixture.Readiness, receipts)
		}}}}, nil
	})
	if settings.App.Scheduler.Enabled {
		builder.Schedules(scheduleDeclarations)
	}
	return builder
}

func scheduleDeclarations(services application.Services) ([]schedule.Declaration, error) {
	tick, err := schedule.Every("acceptance.tick", 10*time.Second, func(ctx context.Context, _ schedule.Invocation) error {
		services.Logger.InfoContext(ctx, "acceptance schedule completed")
		return nil
	})
	return []schedule.Declaration{tick}, err
}

func readiness(ctx context.Context, mode string, receipts *work.Receipts) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch mode {
	case "healthy":
		return nil
	case "fail":
		return fmt.Errorf("fixture dependency is unavailable")
	case "hold":
		released, err := receipts.Released("readiness.release")
		if err != nil {
			return err
		}
		if released {
			return nil
		}
		return fmt.Errorf("fixture dependency is held")
	default:
		return fmt.Errorf("unknown fixture readiness mode")
	}
}

func Seeders() (*seed.Registry, error) {
	return seed.New(seed.Definition{ID: "acceptance.fixture", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO acceptance_fixture (name,value) VALUES ('fixture','ready') ON CONFLICT (name) DO NOTHING`)
		return err
	}})
}
