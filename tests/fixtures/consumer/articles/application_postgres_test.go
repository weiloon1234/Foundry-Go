package articles_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"image"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// fixture is one started application with the article schema migrated into
// its own retained PostgreSQL namespace.
type fixture struct {
	app      *application.App
	services application.Services
	store    *extensions.Store
	schema   string
}

// startApplication assembles the consumer the way an application does: typed
// settings plus Models(articles.FoundryExtensions()...). Migrations are applied
// explicitly; startup never creates schemas or tables.
func startApplication(t *testing.T) fixture {
	t.Helper()
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.Image.Enabled = true
	schema := "articles_" + strings.ToLower(rand.Text())
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	disk := infrastructure.DefaultDiskSettings()
	disk.Local.Root = t.TempDir()
	s.Services.Storage.Default = articles.Files.ID()
	s.Services.Storage.Disks = infrastructure.Disks{articles.Files.ID(): disk}
	s.Features.Extensions.Enabled = true
	s.Features.Extensions.Schema = schema
	s.Features.Locales.Enabled = true
	s.Features.Locales.Locales = []i18n.LocaleID{"en", "ms", "zh"}
	s.Features.Attachments.Enabled = true
	app, err := application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).
		Models(articles.FoundryExtensions()...).
		Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services := app.Resources()
	db, err := services.Database()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	store, err := services.ExtensionStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		for _, target := range app.Migrations() {
			for _, definition := range target.Definitions {
				for _, sql := range definition.SQL {
					if _, err := tx.Exec(ctx, sql); err != nil {
						return err
					}
				}
			}
		}
		for _, sql := range []string{
			`CREATE TABLE article_authors(id uuid PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE articles(id uuid PRIMARY KEY,slug text NOT NULL,author_id uuid NOT NULL REFERENCES article_authors(id),deleted_at timestamptz)`,
		} {
			if _, err := tx.Exec(ctx, sql); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return fixture{app: app, services: services, store: store, schema: schema}
}

// createArticle writes one author and article in the store's schema.
func (f fixture) createArticle(t *testing.T, slug string) articles.Article {
	t.Helper()
	var article articles.Article
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		author, err := articles.QueryArticleAuthors().Create(ctx, tx, articles.AuthorDraft{}.SetName("Author "+slug))
		if err != nil {
			return err
		}
		article, err = articles.QueryArticles().Create(ctx, tx, articles.ArticleDraft{}.SetSlug(slug).SetAuthorID(author.ID))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return article
}

func pngImage(t *testing.T, size int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, size, size))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
