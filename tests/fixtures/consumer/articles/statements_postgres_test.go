package articles_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

// Loading slots costs a constant number of statements per parent batch,
// independent of the number of parents. Direct assembly counts every statement
// of the shared pool, including the extension store's.
func TestSlotLoadingStatementsDoNotGrowWithParents(t *testing.T) {
	var statements atomic.Int64
	store, runtime := openDirect(t, func(context.Context, database.QueryEvent) { statements.Add(1) })
	// Attachment slots stay unbound without an attachments manager.
	x := articles.ArticleExtensions().From(runtime)
	measure := func(limits query.RelationLimits, relations ...query.Relation[articles.Article]) (int64, []articles.Article) {
		t.Helper()
		var loaded []articles.Article
		statements.Store(0)
		if err := store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			var err error
			loaded, err = articles.QueryArticles().WithRelationLimits(limits).With(relations...).All(ctx, tx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, article := range loaded {
			if len(relations) > 0 && (!article.Title.IsLoaded() || !article.Summary.IsLoaded() || !article.SEO.IsLoaded()) {
				t.Fatal("a slot was not loaded")
			}
		}
		return statements.Load(), loaded
	}
	defaults := query.DefaultRelationLimits()
	seedArticles(t, store, runtime, 1)
	base, _ := measure(defaults)
	few, loaded := measure(defaults, x.Title, x.Summary, x.SEO)
	if len(loaded) != 1 {
		t.Fatal("unexpected parent count", len(loaded))
	}
	seedArticles(t, store, runtime, 99)
	many, loaded := measure(defaults, x.Title, x.Summary, x.SEO)
	if len(loaded) != 100 {
		t.Fatal("unexpected parent count", len(loaded))
	}
	// Two parent batches cost exactly two batches of slot statements.
	split := defaults
	split.BatchSize = 50
	batched, _ := measure(split, x.Title, x.Summary, x.SEO)
	t.Logf("slot loading used %d statements for one article, %d for 100 and %d for 100 in two batches (%d without slots)", few, many, batched, base)
	if few != many || batched != base+2*(few-base) {
		t.Fatalf("slot statements are not constant per parent batch: %d, %d, %d (base %d)", few, many, batched, base)
	}
	// Already loaded slots cost no statements under LoadMissing: the read
	// issues exactly what an empty store transaction issues.
	transaction := func(run func(context.Context, *database.Tx) error) int64 {
		t.Helper()
		statements.Store(0)
		if err := store.Read(t.Context(), run); err != nil {
			t.Fatal(err)
		}
		return statements.Load()
	}
	empty := transaction(func(context.Context, *database.Tx) error { return nil })
	missing := transaction(func(ctx context.Context, tx *database.Tx) error {
		_, err := articles.QueryArticles().With(x.Title, x.Summary, x.SEO).LoadMissing(ctx, tx, loaded)
		return err
	})
	if missing != empty {
		t.Fatalf("LoadMissing on loaded slots issued %d statements; an empty transaction issues %d", missing, empty)
	}
	if _, err := articles.QueryArticles().With(x.Logo).All(t.Context(), store.Database()); err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatal("an attachment slot without a manager must report that it is unbound", err)
	}
}
