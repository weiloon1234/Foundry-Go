package articles_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/metadata"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

// openDirect assembles the store, translations and metadata managers directly
// on one pool in a retained namespace, the advanced composition path. An
// optional observer sees every statement of that pool.
func openDirect(tb testing.TB, observer database.QueryObserver) (*extensions.Store, slots.Runtime) {
	tb.Helper()
	config := pgtest.Config(tb)
	adapter, err := postgres.New(config)
	if err != nil {
		tb.Fatal(err)
	}
	var options []database.Option
	if observer != nil {
		options = append(options, database.WithQueryObserver(observer))
	}
	db, err := database.Prepare(adapter, config.Pool, options...)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			tb.Error(err)
		}
	})
	if err := db.Start(tb.Context()); err != nil {
		tb.Fatal(err)
	}
	schema := pgtest.Namespace(tb, db)
	declaration := articles.ArticleExtensionDeclaration()
	registry, err := extensions.NewRegistry(declaration.Owner())
	if err != nil {
		tb.Fatal(err)
	}
	storeConfig := extensions.DefaultConfig()
	storeConfig.Schema = schema
	store, err := extensions.New(db, registry, storeConfig)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			tb.Error(err)
		}
	})
	if err := store.Write(tb.Context(), func(ctx context.Context, tx *database.Tx) error {
		for _, sql := range []string{
			`CREATE TABLE article_authors(id uuid PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE articles(id uuid PRIMARY KEY,slug text NOT NULL,author_id uuid NOT NULL,deleted_at timestamptz)`,
		} {
			if _, err := tx.Exec(ctx, sql); err != nil {
				return err
			}
		}
		for _, definitions := range [][]migrate.Definition{metadata.Migrations(), translations.Migrations()} {
			for _, definition := range definitions {
				for _, sql := range definition.SQL {
					if _, err := tx.Exec(ctx, sql); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		tb.Fatal(err)
	}
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		tb.Fatal(err)
	}
	parts := declaration.Parts()
	var runtime slots.Runtime
	if runtime.Translations, err = translations.New(store, locales, parts.Translations...); err != nil {
		tb.Fatal(err)
	}
	if runtime.Metadata, err = metadata.New(store, parts.Metadata...); err != nil {
		tb.Fatal(err)
	}
	return store, runtime
}

// seedArticles writes count articles with two translated fields and metadata.
func seedArticles(tb testing.TB, store *extensions.Store, runtime slots.Runtime, count int) {
	tb.Helper()
	x := articles.ArticleExtensions().From(runtime)
	for i := range count {
		var article articles.Article
		if err := store.Write(tb.Context(), func(ctx context.Context, tx *database.Tx) error {
			author, err := articles.QueryArticleAuthors().Create(ctx, tx, articles.AuthorDraft{}.SetName("Author"))
			if err != nil {
				return err
			}
			article, err = articles.QueryArticles().Create(ctx, tx, articles.ArticleDraft{}.SetSlug(fmt.Sprintf("slug-%05d", i)).SetAuthorID(author.ID))
			if err != nil {
				return err
			}
			if err := x.Title.SaveIn(ctx, tx, article, map[i18n.LocaleID]string{"en": "Title", "ms": "Tajuk"}); err != nil {
				return err
			}
			if err := x.Summary.SaveIn(ctx, tx, article, map[i18n.LocaleID]string{"en": "Summary"}); err != nil {
				return err
			}
			return x.SEO.SaveIn(ctx, tx, article, articles.SEO{Canonical: value.Set("/" + article.Slug)})
		}); err != nil {
			tb.Fatal(err)
		}
	}
}
