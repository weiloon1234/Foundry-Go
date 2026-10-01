package articles_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/outbox"
	redistest "github.com/weiloon1234/Foundry-Go/testkit/redis"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

// durableCleanup runs cleanup jobs through a Redis job connection, the
// outbox in the fixture's schema and a worker.
func durableCleanup(connection infrastructure.RedisConnectionSettings) func(*application.Settings) {
	return func(s *application.Settings) {
		s.Services.Namespace.Application = "articles-" + strings.ToLower(rand.Text())
		s.Services.Redis.Connections = infrastructure.RedisConnections{"default": connection}
		queue := infrastructure.DefaultJobConnectionSettings()
		queue.Driver = infrastructure.RedisJobs
		s.Services.Jobs.Connections = infrastructure.JobConnections{"default": queue}
		s.Worker.Enabled = true
		s.Worker.Config.PollInterval = time.Millisecond
		s.Features.Outbox.Enabled = true
		s.Features.Outbox.Schema = s.Features.Extensions.Schema
		s.Features.Outbox.PollInterval = 5 * time.Millisecond
		s.Features.Outbox.Jobs = map[jobs.ConnectionName]outbox.Destination{"default": "extensions"}
		s.Features.ExtensionCleanup.Jobs = "default"
	}
}

// A connection to another database could not deliver the cleanup rows it
// writes, so startup refuses it before any deletion runs.
func TestDurableCleanupRefusesAConnectionToAnotherDatabase(t *testing.T) {
	s, _ := articleSettings(t, durableCleanup(infrastructure.RedisSettingsFromConfig(redistest.Config(t))), func(s *application.Settings) {
		elsewhere := s.Services.Database.Connections["default"]
		if elsewhere.Primary.Database == "postgres" {
			elsewhere.Primary.Database = "template1"
		} else {
			elsewhere.Primary.Database = "postgres"
		}
		s.Services.Database.Connections["elsewhere"] = elsewhere
	})
	app, err := application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Models(articles.FoundryExtensions()...).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})
	if err := app.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "database connection elsewhere does not reach the outbox database") {
		t.Fatal("startup accepted a connection to another database", err)
	}
}

// Direct assembly declares the cleanup job for a runtime with an extension
// store and binds one outbox producer per pool; each is refused otherwise.
func TestCleanupJobDirectAssembly(t *testing.T) {
	job := slots.DefineCleanupJob("consumer.extensions.cleanup", jobs.DefaultPolicy("default"))
	if _, err := job.Declare(slots.Runtime{}); err == nil {
		t.Fatal("cleanup job declared without an extension store")
	}
	if _, err := job.ToOutbox("default", nil); err == nil {
		t.Fatal("cleanup queue accepted no producers")
	}
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := job.Declare(runtime); err != nil {
		t.Fatal(err)
	}
}

// features.extension_cleanup is refused at Build, before any resource opens,
// unless the outbox carries its job connection.
func TestDurableCleanupRequiresTheOutbox(t *testing.T) {
	for name, test := range map[string]struct {
		configure func(*application.Settings)
		want      string
	}{
		"outbox disabled": {func(*application.Settings) {}, "requires features.extensions and features.outbox"},
		"no destination":  {func(s *application.Settings) { s.Features.Outbox.Enabled = true }, "has no features.outbox.jobs destination"},
	} {
		t.Run(name, func(t *testing.T) {
			s := application.DefaultSettings()
			s.HTTP.Enabled = false
			s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": infrastructure.DefaultConnectionSettings()}
			s.Features.Extensions.Enabled = true
			s.Features.ExtensionCleanup.Jobs = "default"
			test.configure(&s)
			_, err := application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Build(t.Context())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

// With features.extension_cleanup, a model deleted through another connection
// enqueues its cleanup job in the deletion's own transaction through the
// outbox: a rollback leaves no job, and a committed deletion's job waits in the
// outbox, surviving the process, until a worker delivers it.
func TestDurableCleanupAfterDeletionThroughAnotherPool(t *testing.T) {
	f := startApplication(t, durableCleanup(infrastructure.RedisSettingsFromConfig(redistest.Config(t))), withWriter)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.CleanupQueue != nil {
		t.Fatal("descriptor runtimes must not carry the cleanup queue")
	}
	x := articles.ArticleExtensions().From(runtime)
	article := f.createArticle(t, "durable")
	ref := article.FoundryReference()
	if err := x.Title.Field().Set(t.Context(), runtime.Translations, ref, "en", "Hello"); err != nil {
		t.Fatal(err)
	}
	if err := x.SEO.Key().Set(t.Context(), runtime.Metadata, ref, articles.SEO{Canonical: value.Set("/hello")}); err != nil {
		t.Fatal(err)
	}
	upload, err := x.Logo.Collection().Replace(t.Context(), runtime.Attachments, ref, attachments.Upload{Source: bytes.NewReader(pngImage(t, 40)), OriginalName: "logo.png"})
	if err != nil || upload.Publication != attachments.Published {
		t.Fatal("logo publication", err)
	}
	queued := func() int {
		t.Helper()
		var count int
		if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			return database.ScanOne(ctx, tx, `SELECT count(*) FROM foundry_outbox WHERE name = 'foundry.extensions.cleanup'`, nil, &count)
		}); err != nil {
			t.Fatal(err)
		}
		return count
	}
	orphans := func() int {
		t.Helper()
		owner := articles.ArticleExtensionOwner().Name()
		meta, err := metadata.InspectOrphans(t.Context(), runtime.Metadata, owner, metadata.Cursor{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		text, err := translations.InspectOrphans(t.Context(), runtime.Translations, owner, translations.Cursor{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		return len(meta.Orphans) + len(text.Orphans)
	}

	rollback := errors.New("rollback deletion")
	if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error {
		if _, err := articles.QueryArticles().ForceDelete(ctx, tx, article.ID); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if queued() != 0 {
		t.Fatal("a rolled-back deletion left a cleanup job")
	}

	// No worker or publisher runs yet: the committed deletion leaves exactly one
	// outbox row, and its data waits for it, as after a crash.
	if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error {
		_, err := articles.QueryArticles().ForceDelete(ctx, tx, article.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if queued() != 1 || orphans() != 2 {
		t.Fatal("the committed deletion did not leave its job and data for the worker", queued(), orphans())
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.app.Run(ctx, foundation.Worker) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("worker did not drain")
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for orphans() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the cleanup job did not remove the deleted owner's data")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		state, err := runtime.Attachments.Inspect(t.Context(), upload.Operation)
		if err != nil {
			t.Fatal(err)
		}
		if state.State == attachments.Cleaned {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the cleanup job kept the logo", state.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
