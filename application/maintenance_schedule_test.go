package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/jobarchive"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type lockedLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}
func (l *lockedLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }

func housekeepingSettings() application.Settings {
	s := settings()
	s.HTTP.Enabled = false
	s.Scheduler.Enabled = true
	s.Services.Coordination.Enabled = true
	s.Features.Maintenance.Enabled = true
	return s
}

func scheduleIDs(t *testing.T, app *application.App) []schedule.ID {
	t.Helper()
	scheduler, err := app.Resources().Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	var ids []schedule.ID
	for _, d := range scheduler.Describe() {
		ids = append(ids, d.ID)
	}
	return ids
}

func TestDeclaredPruningRunsAsBoundedLeaderSchedules(t *testing.T) {
	s := housekeepingSettings()
	s.Features.Maintenance.Custom.Batch = 2
	s.Features.Maintenance.Custom.MaxBatches = 3
	s.Features.Maintenance.Custom.Interval = 10 * time.Minute
	var output lockedLog
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	var limits []int
	var mu sync.Mutex
	backlog := 7
	app, err := application.New(s, application.WithLogger(logger)).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Pruning: []application.Pruning{application.PruneWith("app.challenges", 16, func(ctx context.Context, limit int) (int64, error) {
			mu.Lock()
			defer mu.Unlock()
			limits = append(limits, limit)
			removed := min(limit, backlog)
			backlog -= removed
			return int64(removed), nil
		})}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	scheduler, err := app.Resources().Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	descriptions := scheduler.Describe()
	if len(descriptions) != 1 || descriptions[0].ID != "foundry.maintenance.app.challenges" || descriptions[0].Interval != 10*time.Minute || !descriptions[0].WithoutOverlap || descriptions[0].Timeout != 5*time.Minute {
		t.Fatal("declared pruning was not scheduled with its settings", descriptions)
	}
	// A stable offset inside the interval spreads tasks away from the hour.
	if offset := descriptions[0].Anchor.Sub(time.Unix(0, 0)); offset < 0 || offset >= 10*time.Minute || offset%time.Second != 0 {
		t.Fatal("housekeeping anchor is not a whole-second offset within its interval", descriptions[0].Anchor)
	}
	// MaxBatches stops a run with work remaining; the next run continues.
	for _, want := range []int{3, 4} {
		record, err := scheduler.RunNow(t.Context(), "foundry.maintenance.app.challenges")
		if err != nil || record.State != schedule.Succeeded {
			t.Fatal("pruning run failed", record, err)
		}
		mu.Lock()
		calls := len(limits)
		mu.Unlock()
		if calls != want {
			t.Fatal("pruning did not stop at MaxBatches or a short batch", calls)
		}
	}
	if backlog != 0 || slices.ContainsFunc(limits, func(limit int) bool { return limit != 2 }) {
		t.Fatal("pruning ignored the configured batch", backlog, limits)
	}
	text := output.String()
	if !strings.Contains(text, `"msg":"maintenance task pruned records","task":"app.challenges","removed":6,"remaining":true`) || !strings.Contains(text, `"removed":1,"remaining":false`) {
		t.Fatal("pruning outcomes were not logged", text)
	}
}

func TestMaintenanceScheduleIsOptInAndValidatedBeforeResources(t *testing.T) {
	declare := func(ran *atomic.Int32) application.Features {
		return func(application.Services) (application.FeatureDeclarations, error) {
			return application.FeatureDeclarations{Pruning: []application.Pruning{application.PruneWith("app.tokens", 10, func(context.Context, int) (int64, error) { ran.Add(1); return 0, nil })}}, nil
		}
	}
	var ran atomic.Int32
	for name, mutate := range map[string]func(*application.Settings){
		"disabled":          func(s *application.Settings) { s.Features.Maintenance.Enabled = false },
		"task disabled":     func(s *application.Settings) { s.Features.Maintenance.Custom.Disabled = true },
		"no scheduler here": func(s *application.Settings) { s.Scheduler.Enabled = false; s.Services.Coordination.Enabled = false },
	} {
		t.Run(name, func(t *testing.T) {
			s := housekeepingSettings()
			mutate(&s)
			builder := application.New(s, quiet()).Features(declare(&ran))
			if s.Scheduler.Enabled {
				// A scheduler needs at least one declaration of its own.
				builder.Schedules(func(application.Services) ([]schedule.Declaration, error) {
					d, err := schedule.Every("app.report", time.Hour, func(context.Context, schedule.Invocation) error { return nil })
					return []schedule.Declaration{d}, err
				})
			}
			app, err := builder.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stop(t, app) })
			if !s.Scheduler.Enabled {
				return
			}
			if ids := scheduleIDs(t, app); !slices.Equal(ids, []schedule.ID{"app.report"}) {
				t.Fatal("inactive pruning was scheduled", ids)
			}
		})
	}
	for name, mutate := range map[string]func(*application.Settings){
		"session batch":  func(s *application.Settings) { s.Features.Maintenance.Sessions.Batch = 5000 },
		"interval":       func(s *application.Settings) { s.Features.Maintenance.Defaults.Interval = time.Millisecond },
		"timeout":        func(s *application.Settings) { s.Features.Maintenance.Defaults.Timeout = 48 * time.Hour },
		"max batches":    func(s *application.Settings) { s.Features.Maintenance.Defaults.MaxBatches = -1 },
		"custom batch":   func(s *application.Settings) { s.Features.Maintenance.Custom.Batch = 11 },
		"outbox retain":  func(s *application.Settings) { enableOutbox(s); s.Features.Maintenance.Outbox.Retention = -time.Hour },
		"scheduler-less": func(s *application.Settings) { s.Scheduler.Enabled = false; s.Features.Maintenance.Defaults.Batch = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			s := housekeepingSettings()
			mutate(&s)
			if _, err := application.New(s, quiet()).Features(declare(&ran)).Build(t.Context()); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid maintenance settings accepted", err)
			}
		})
	}
	if ran.Load() != 0 {
		t.Fatal("building ran a pruning task")
	}
	s := housekeepingSettings()
	invalid := func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Pruning: []application.Pruning{application.PruneWith("Invalid Name", 10, func(context.Context, int) (int64, error) { return 0, nil })}}, nil
	}
	if _, err := application.New(s, quiet()).Features(invalid).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid pruning declaration accepted", err)
	}
	duplicate := func(application.Services) (application.FeatureDeclarations, error) {
		task := application.PruneWith("app.twice", 10, func(context.Context, int) (int64, error) { return 0, nil })
		return application.FeatureDeclarations{Pruning: []application.Pruning{task, task}}, nil
	}
	if _, err := application.New(s, quiet()).Features(duplicate).Build(t.Context()); err == nil {
		t.Fatal("duplicate pruning task accepted")
	}
}

func enableOutbox(s *application.Settings) {
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": infrastructure.DefaultConnectionSettings()}
	s.Features.Outbox.Enabled = true
}

func TestMaintenanceSchedulePrunesConfiguredFrameworkStores(t *testing.T) {
	schema := pgtest.Namespace(t, pgtest.Open(t))
	now := time.Now().UTC().Truncate(time.Second)
	source := testkit.NewClock(now)
	s := housekeepingSettings()
	databaseConfig := infrastructure.DefaultConnectionSettings()
	databaseConfig.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	// Prunable model selections resolve their tables through the pool's path.
	databaseConfig.Primary.Schema = schema
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": databaseConfig}
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": infrastructure.DefaultJobConnectionSettings()}
	s.Worker.Archive = application.JobArchiveSettings{Enabled: true, Schema: schema}
	s.Features.Outbox.Enabled, s.Features.Outbox.Schema = true, schema
	s.Features.Notifications.Enabled, s.Features.Notifications.Schema = true, schema
	s.Features.Audit.Enabled, s.Features.Audit.Schema, s.Features.Audit.Config.RetentionDays = true, schema, 30
	s.Features.Idempotency.Enabled, s.Features.Idempotency.Config.Schema = true, schema
	routes := func(services application.Services) (application.FeatureDeclarations, error) {
		// An application-declared prunable model with a cutoff relative to each run.
		models, err := application.PruneModels(services, "", func(now time.Time) ([]prune.Target, error) {
			cutoff, err := temporal.NewDateTime(now.Add(-10 * 24 * time.Hour))
			return []prune.Target{prune.Model("app.stale_failures", jobarchive.QueryFoundryFailedJobs().Where(jobarchive.FailureFields().FailedAt.Lt(cutoff)).Query, prune.Mass)}, err
		})
		return application.FeatureDeclarations{Outbox: []publisher.Route{{Kind: "maintenance.fixture", Destination: "primary", Publish: func(context.Context, publisher.Message) error { return nil }}}, Pruning: []application.Pruning{models}}, err
	}
	// The store-owned idempotency pruner stays in charge unless it is disabled.
	app, err := application.New(s, quiet(), application.WithClock(source)).Features(routes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ids := scheduleIDs(t, app)
	stop(t, app)
	if slices.Contains(ids, "foundry.maintenance.idempotency") {
		t.Fatal("idempotency would be pruned twice", ids)
	}
	s.Features.Idempotency.Config.PruneInterval = 0
	app, err = application.New(s, quiet(), application.WithClock(source)).Features(routes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	migrated := false
	// The explicit migration task uses the ordinary runner (one index is built
	// concurrently) on a pool whose search path is the test namespace.
	schemaDB := pgtest.Open(t, func(c *postgres.Config) { c.Schema = schema })
	for _, target := range app.Migrations() {
		if target.Schema != schema {
			continue
		}
		migrated = migrated || slices.ContainsFunc(target.Definitions, func(d migrate.Definition) bool { return d.Key.Origin == archive.MigrationOrigin })
		registry, err := migrate.New(target.Definitions...)
		if err != nil {
			t.Fatal(err)
		}
		config := migrate.DefaultPostgresConfig()
		config.Schema = schema
		runner, err := migrate.NewPostgres(schemaDB, registry, config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Up(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if !migrated {
		t.Fatal("app.Migrations omitted the enabled job archive")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []schedule.ID{"foundry.maintenance.audit", "foundry.maintenance.idempotency", "foundry.maintenance.jobs.archive", "foundry.maintenance.models.default", "foundry.maintenance.notifications.inbox", "foundry.maintenance.outbox"}
	if ids := scheduleIDs(t, app); !slices.Equal(ids, want) {
		t.Fatal("framework stores were not scheduled", ids)
	}
	store, err := app.Resources().JobArchive()
	if err != nil {
		t.Fatal(err)
	}
	definition := jobs.Define[assemblyPayload]("maintenance-archive", 1, jobs.DefaultPolicy("reports"))
	for _, age := range []time.Duration{45 * 24 * time.Hour, 20 * 24 * time.Hour, time.Hour} {
		pending, err := definition.Capture(t.Context(), assemblyPayload{"archived"}, jobs.Options[assemblyPayload]{})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.RecordFailure(t.Context(), jobs.FailedJob{Queue: "reports", Envelope: pending.Envelope(), Reason: jobs.HandlerFailed, Attempts: 1, FailedAt: now.Add(-age)}); err != nil {
			t.Fatal(err)
		}
	}
	scheduler, err := app.Resources().Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	remaining := func() int {
		t.Helper()
		page, err := store.List(t.Context(), archive.ListOptions{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Entries)
	}
	for _, id := range want {
		if record, err := scheduler.RunNow(t.Context(), id); err != nil || record.State != schedule.Succeeded {
			t.Fatal("framework pruning failed", id, record, err)
		}
		// The archive's 30-day retention runs before the 10-day model selection.
		if id == "foundry.maintenance.jobs.archive" && remaining() != 2 {
			t.Fatal("job archive retention was not applied", remaining())
		}
	}
	if remaining() != 1 {
		t.Fatal("prunable model selection was not applied", remaining())
	}
}

func TestGuardPruningRemovesExpiredSessionsAndTokens(t *testing.T) {
	schema := pgtest.Namespace(t, pgtest.Open(t))
	schemaDB := pgtest.Open(t, func(c *postgres.Config) { c.Schema = schema })
	source := testkit.NewClock(time.Now().UTC().Truncate(time.Second))
	s := housekeepingSettings()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	s.Features.Auth.Sessions.Enabled, s.Features.Auth.Sessions.Schema = true, schema
	s.Features.Auth.Tokens.Enabled, s.Features.Auth.Tokens.Schema = true, schema
	provider := auth.DefineProvider("fixture.operators", (reportOperator{}).FoundryReference(), func(_ context.Context, id int64) (value.Optional[reportOperator], error) {
		return value.Set(reportOperator{ID: id}), nil
	}, func(context.Context, reportOperator) (bool, error) { return true, nil })
	var browser application.BrowserGuard[reportOperator, int64]
	var bearer application.TokenGuard[reportOperator, int64]
	app, err := application.New(s, quiet(), application.WithClock(source)).Features(func(services application.Services) (application.FeatureDeclarations, error) {
		var err error
		if browser, err = application.NewBrowserGuard(services, "", provider, "operator.cookie"); err != nil {
			return application.FeatureDeclarations{}, err
		}
		allowed, err := auth.NewAccessScopes[reportOperator]()
		if err != nil {
			return application.FeatureDeclarations{}, err
		}
		if bearer, err = application.NewTokenGuard(services, "", provider, "operator.bearer", allowed); err != nil {
			return application.FeatureDeclarations{}, err
		}
		return application.FeatureDeclarations{Pruning: []application.Pruning{application.PruneSessions(browser), application.PruneTokens(bearer)}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	for _, target := range app.Migrations() {
		registry, err := migrate.New(target.Definitions...)
		if err != nil {
			t.Fatal(err)
		}
		config := migrate.DefaultPostgresConfig()
		config.Schema = schema
		runner, err := migrate.NewPostgres(schemaDB, registry, config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Up(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if ids := scheduleIDs(t, app); !slices.Equal(ids, []schedule.ID{"foundry.maintenance.sessions.web", "foundry.maintenance.tokens.api"}) {
		t.Fatal("guard pruning was not scheduled", ids)
	}
	proof, err := auth.NewProof(reportOperator{ID: 7}.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := browser.Sessions.Issue(t.Context(), proof, session.IssueOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := bearer.Tokens.Issue(t.Context(), proof, token.IssueOptions[reportOperator]{Name: "fixture"}); err != nil {
		t.Fatal(err)
	}
	rows := func(table string) int {
		t.Helper()
		result, err := schemaDB.Query(t.Context(), `SELECT count(*) FROM "`+schema+`".`+table)
		if err != nil {
			t.Fatal(err)
		}
		defer result.Close()
		count := -1
		if !result.Next() || result.Scan(&count) != nil {
			t.Fatal("count failed", result.Err())
		}
		return count
	}
	scheduler, err := app.Resources().Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	run := func() {
		for _, id := range []schedule.ID{"foundry.maintenance.sessions.web", "foundry.maintenance.tokens.api"} {
			if record, err := scheduler.RunNow(t.Context(), id); err != nil || record.State != schedule.Succeeded {
				t.Fatal("guard pruning failed", id, record, err)
			}
		}
	}
	run()
	if rows("foundry_sessions") != 1 || rows("foundry_token_families") != 1 {
		t.Fatal("live credentials were pruned")
	}
	source.Advance(90 * 24 * time.Hour)
	run()
	if rows("foundry_sessions") != 0 || rows("foundry_token_families") != 0 {
		t.Fatal("expired credentials survived pruning")
	}
}
