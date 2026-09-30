package articles_test

import (
	"context"
	"fmt"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

// BenchmarkSlotListLoading measures one list read of two translated fields
// and one metadata value at 1, 100 and 1000 parents: parents alone, slots
// loaded through With, and the same data through explicit batch loads. Run it
// with make-style native PostgreSQL; results depend on host and database.
func BenchmarkSlotListLoading(b *testing.B) {
	store, runtime := openDirect(b, nil)
	x := articles.ArticleExtensions().From(runtime)
	seedArticles(b, store, runtime, 1000)
	fields := articles.ArticleFields()
	read := func(b *testing.B, run func(context.Context, *database.Tx) error) {
		b.Helper()
		if err := store.Read(b.Context(), run); err != nil {
			b.Fatal(err)
		}
	}
	for _, size := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("parents-only/%d", size), func(b *testing.B) {
			for b.Loop() {
				read(b, func(ctx context.Context, tx *database.Tx) error {
					_, err := articles.QueryArticles().OrderBy(fields.Slug.Asc()).Limit(size).All(ctx, tx)
					return err
				})
			}
		})
		b.Run(fmt.Sprintf("slots/%d", size), func(b *testing.B) {
			for b.Loop() {
				read(b, func(ctx context.Context, tx *database.Tx) error {
					_, err := articles.QueryArticles().With(x.Title, x.Summary, x.SEO).OrderBy(fields.Slug.Asc()).Limit(size).All(ctx, tx)
					return err
				})
			}
		})
		b.Run(fmt.Sprintf("explicit-batches/%d", size), func(b *testing.B) {
			for b.Loop() {
				var list []articles.Article
				read(b, func(ctx context.Context, tx *database.Tx) error {
					var err error
					list, err = articles.QueryArticles().OrderBy(fields.Slug.Asc()).Limit(size).All(ctx, tx)
					return err
				})
				references := make([]model.Reference[articles.Article, model.ID[articles.Article]], len(list))
				for i, article := range list {
					references[i] = article.FoundryReference()
				}
				// Resolve every owner from each batch, as the slot path does
				// when it fills parents, so both variants do equivalent work.
				titles, err := x.Title.Field().Load(b.Context(), runtime.Translations, references)
				if err != nil {
					b.Fatal(err)
				}
				summaries, err := x.Summary.Field().Load(b.Context(), runtime.Translations, references)
				if err != nil {
					b.Fatal(err)
				}
				seo, err := x.SEO.Key().Load(b.Context(), runtime.Metadata, references)
				if err != nil {
					b.Fatal(err)
				}
				for _, reference := range references {
					if _, err := titles.Get(reference); err != nil {
						b.Fatal(err)
					}
					if _, err := summaries.Get(reference); err != nil {
						b.Fatal(err)
					}
					if _, err := seo.Get(b.Context(), reference); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
