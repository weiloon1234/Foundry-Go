package articles_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/articles"
	"foundry.test/consumer/internal/queryfixture"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// seeded is one application with a fully populated and an empty article.
type seeded struct {
	fixture
	x           articles.ArticleExtensionSlots
	full, empty articles.Article
}

func seed(t *testing.T) seeded {
	t.Helper()
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleExtensions().From(runtime)
	full, empty := f.createArticle(t, "a-full"), f.createArticle(t, "b-empty")
	ref := full.FoundryReference()
	for locale, text := range map[i18n.LocaleID]string{"en": "Hello", "ms": "Helo"} {
		if err := x.Title.Field().Set(t.Context(), runtime.Translations, ref, locale, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := x.Summary.Field().Set(t.Context(), runtime.Translations, ref, "en", ""); err != nil {
		t.Fatal(err)
	}
	if err := x.SEO.Key().Set(t.Context(), runtime.Metadata, ref, articles.SEO{Canonical: value.Set("/hello"), Keywords: value.Set([]string{"go"})}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Logo.Collection().Replace(t.Context(), runtime.Attachments, ref, attachments.Upload{Source: bytes.NewReader(pngImage(t, 40)), OriginalName: "logo.png"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.png", "second.png"} {
		if _, err := x.Galleries.Collection().Add(t.Context(), runtime.Attachments, ref, attachments.Upload{Source: bytes.NewReader(pngImage(t, 24)), OriginalName: name}); err != nil {
			t.Fatal(err)
		}
	}
	return seeded{fixture: f, x: x, full: full, empty: empty}
}

func (s seeded) read(t *testing.T, run func(ctx context.Context, tx *database.Tx) error) {
	t.Helper()
	if err := s.store.Read(t.Context(), run); err != nil {
		t.Fatal(err)
	}
}

// One With call loads relations and every slot kind; each slot distinguishes
// not loaded, loaded empty and loaded present without further I/O.
func TestSlotsLoadThroughWith(t *testing.T) {
	s := seed(t)
	fields := articles.ArticleFields()
	for name, executor := range map[string]func(*database.Tx) database.Executor{
		"joined transaction": func(tx *database.Tx) database.Executor { return tx },
		"store snapshot":     func(tx *database.Tx) database.Executor { return &queryfixture.QueryCounter{Executor: tx} },
	} {
		t.Run(name, func(t *testing.T) {
			var list []articles.Article
			s.read(t, func(ctx context.Context, tx *database.Tx) error {
				var err error
				list, err = articles.QueryArticles().
					With(articles.ArticleRelations().Author, s.x.Title, s.x.Summary, s.x.Logo, s.x.Galleries, s.x.SEO).
					OrderBy(fields.Slug.Asc()).All(ctx, executor(tx))
				return err
			})
			if len(list) != 2 {
				t.Fatal("unexpected article count", len(list))
			}
			full, empty := list[0], list[1]
			if author, loaded := full.Author.Get(); !loaded || !author.IsSet() {
				t.Fatal("ordinary relation was not loaded beside slots")
			}
			exact, err := full.Title.Exact("ms")
			if text, ok := exact.Get(); err != nil || !ok || text != "Helo" {
				t.Fatal("exact translated text", err)
			}
			fallback, err := full.Title.Resolve("zh")
			if resolved, ok := fallback.Get(); err != nil || !ok || resolved.Locale != "en" || resolved.Text != "Hello" {
				t.Fatal("translated fallback", err)
			}
			request, err := full.Title.ResolveRequest(t.Context())
			if resolved, ok := request.Get(); err != nil || !ok || resolved.Locale != "en" {
				t.Fatal("request locale fallback to the catalog default", err)
			}
			summary, err := full.Summary.Exact("en")
			if text, ok := summary.Get(); err != nil || !ok || text != "" {
				t.Fatal("empty text must be a present translation", err)
			}
			logo, loaded := full.Logo.Get()
			if file, ok := logo.Get(); !loaded || !ok || file.Info().Width != 32 || file.Collection() != "logo" {
				t.Fatal("single attachment slot", loaded, ok)
			}
			if full.Galleries.Len() != 2 {
				t.Fatal("gallery slot size", full.Galleries.Len())
			}
			var names []string
			for _, file := range full.Galleries.All() {
				names = append(names, file.Info().OriginalName)
			}
			if len(names) != 2 || names[0] != "first.png" || names[1] != "second.png" {
				t.Fatal("gallery order", names)
			}
			seo, loaded := full.SEO.Get()
			if stored, ok := seo.Get(); !loaded || !ok {
				t.Fatal("metadata slot", loaded, ok)
			} else if canonical, _ := stored.Canonical.Get(); canonical != "/hello" {
				t.Fatal("metadata value", canonical)
			}

			// Loaded but empty differs from not loaded.
			if values, loaded := empty.Title.Values(); !loaded || len(values.Entries()) != 0 {
				t.Fatal("empty translated slot")
			}
			if resolved, err := empty.Title.Resolve("en"); err != nil || resolved.IsSet() {
				t.Fatal("absent translation resolved", err)
			}
			if logo, loaded := empty.Logo.Get(); !loaded || logo.IsSet() {
				t.Fatal("empty single attachment slot")
			}
			if files, loaded := empty.Galleries.Get(); !loaded || len(files) != 0 {
				t.Fatal("empty gallery slot")
			}
			if seo, loaded := empty.SEO.Get(); !loaded || seo.IsSet() {
				t.Fatal("empty metadata slot")
			}
		})
	}
	var plain articles.Article
	s.read(t, func(ctx context.Context, tx *database.Tx) error {
		var err error
		plain, err = articles.QueryArticles().RequireFind(ctx, tx, s.full.ID)
		return err
	})
	if plain.Title.IsLoaded() || plain.Galleries.IsLoaded() {
		t.Fatal("slots loaded without With")
	}
	if _, err := plain.Title.Exact("en"); !errors.Is(err, fault.Missing) {
		t.Fatal("reading an unloaded slot must report Missing", err)
	}
}

// Links derive from loaded slots without I/O and follow the collection's own
// link rules; an unloaded slot fails instead of reading storage.
func TestSlotLinksMatchCollectionLinks(t *testing.T) {
	s := seed(t)
	runtime, err := s.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	var full, empty articles.Article
	s.read(t, func(ctx context.Context, tx *database.Tx) error {
		loaded, err := articles.QueryArticles().With(s.x.Logo, s.x.Galleries).OrderBy(articles.ArticleFields().Slug.Asc()).All(ctx, tx)
		if err == nil {
			full, empty = loaded[0], loaded[1]
		}
		return err
	})
	stored, err := s.x.Logo.Collection().List(t.Context(), runtime.Attachments, full.FoundryReference())
	if err != nil || len(stored) != 1 {
		t.Fatal("explicit list", err)
	}
	_, want := s.x.Logo.Collection().PublicURLOf(t.Context(), runtime.Attachments, stored[0])
	_, got := s.x.Logo.PublicURL(t.Context(), full)
	if (want == nil) != (got == nil) || (want != nil && want.Error() != got.Error()) {
		t.Fatal("slot link differs from the collection link", want, got)
	}
	if url, err := s.x.Logo.PublicURL(t.Context(), empty); err != nil || url.IsSet() {
		t.Fatal("an empty loaded slot has no link", err)
	}
	if _, err := s.x.Logo.PublicURL(t.Context(), s.full); !errors.Is(err, fault.Missing) {
		t.Fatal("an unloaded slot produced a link", err)
	}
	file, _ := full.Galleries.Get()
	_, want = s.x.Galleries.Collection().VariantPublicURLOf(t.Context(), runtime.Attachments, galleryAttachment(t, s, runtime, file[0].ID()), articles.Thumbnail)
	_, got = s.x.Galleries.VariantURL(t.Context(), full, file[0], articles.Thumbnail)
	if (want == nil) != (got == nil) || (want != nil && want.Error() != got.Error()) {
		t.Fatal("slot variant link differs from the collection link", want, got)
	}
}

// Relation loading paths reuse the slot descriptors: explicit Load and
// LoadMissing, pagination, chunking and nested relations.
func TestSlotsLoadThroughEveryRelationPath(t *testing.T) {
	s := seed(t)
	fields := articles.ArticleFields()
	s.read(t, func(ctx context.Context, tx *database.Tx) error {
		loaded, err := articles.QueryArticles().With(s.x.Title).Load(ctx, tx, []articles.Article{s.full, s.full})
		if err != nil {
			return err
		}
		if len(loaded) != 2 || !loaded[0].Title.IsLoaded() || !loaded[1].Title.IsLoaded() || s.full.Title.IsLoaded() {
			t.Fatal("explicit Load must fill copies, including repeated parents")
		}
		// LoadMissing's zero-statement case is measured with a statement
		// observer in TestSlotLoadingStatementsDoNotGrowWithParents.
		missing, err := articles.QueryArticles().With(s.x.Title).LoadMissing(ctx, tx, loaded)
		if err != nil {
			return err
		}
		if !missing[0].Title.IsLoaded() {
			t.Fatal("LoadMissing dropped a loaded slot")
		}
		page, err := articles.QueryArticles().With(s.x.SEO).OrderBy(fields.Slug.Asc()).Paginate(ctx, tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if len(page.Items) != 1 || !page.Items[0].SEO.IsLoaded() || page.Total != 2 {
			t.Fatal("paginated slot loading")
		}
		simple, err := articles.QueryArticles().With(s.x.SEO).OrderBy(fields.Slug.Asc()).SimplePaginate(ctx, tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if len(simple.Items) != 1 || !simple.Items[0].SEO.IsLoaded() || !simple.HasMore {
			t.Fatal("simple paginated slot loading")
		}
		cursor, err := articles.QueryArticles().With(s.x.Title).OrderBy(fields.Slug.Asc()).CursorPaginate(ctx, tx, query.CursorRequest[articles.Article]{Size: 1})
		if err != nil {
			return err
		}
		next, more := cursor.Next.Get()
		if len(cursor.Items) != 1 || !cursor.Items[0].Title.IsLoaded() || !more {
			t.Fatal("cursor paginated slot loading")
		}
		following, err := articles.QueryArticles().With(s.x.Title).OrderBy(fields.Slug.Asc()).CursorPaginate(ctx, tx, query.CursorRequest[articles.Article]{Size: 1, After: value.Set(next)})
		if err != nil {
			return err
		}
		if len(following.Items) != 1 || !following.Items[0].Title.IsLoaded() || following.Items[0].ID == cursor.Items[0].ID {
			t.Fatal("second cursor page slot loading")
		}
		chunked := 0
		if err := articles.QueryArticles().With(s.x.Galleries).EachChunked(ctx, tx, 1, func(article articles.Article) error {
			if !article.Galleries.IsLoaded() {
				t.Fatal("chunked slot loading")
			}
			chunked++
			return nil
		}); err != nil {
			return err
		}
		if chunked != 2 {
			t.Fatal("chunk count", chunked)
		}
		authors, err := articles.QueryArticleAuthors().With(articles.AuthorRelations().Articles.With(s.x.Title)).All(ctx, tx)
		if err != nil {
			return err
		}
		nested := 0
		for _, author := range authors {
			if author.Articles.Len() != 1 {
				t.Fatal("each seeded author owns one article", author.Articles.Len())
			}
			for _, article := range author.Articles.All() {
				if !article.Title.IsLoaded() {
					t.Fatal("nested relation did not load the slot")
				}
				nested++
			}
		}
		if len(authors) != 2 || nested != 2 {
			t.Fatal("nested slot loading visited", len(authors), nested)
		}
		return nil
	})
}

// Visibility, binding and budgets fail or stay unloaded as documented.
func TestSlotLoadingBoundaries(t *testing.T) {
	s := seed(t)
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := articles.QueryArticles().Delete(ctx, tx, s.empty.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.read(t, func(ctx context.Context, tx *database.Tx) error {
		trashed, err := articles.QueryArticles().OnlyTrashed().With(s.x.Title, s.x.Galleries).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(trashed) != 1 || trashed[0].Title.IsLoaded() || trashed[0].Galleries.IsLoaded() {
			t.Fatal("a soft-deleted owner's slots must stay unloaded")
		}
		if _, err := articles.QueryArticles().With(articles.ArticleExtensions().Title).All(ctx, tx); !errors.Is(err, fault.Missing) {
			t.Fatal("an unbound slot must fail before loading", err)
		}
		limited := query.RelationLimits{BatchSize: 500, MaxRows: 1, MaxDepth: 8}
		if _, err := articles.QueryArticles().WithRelationLimits(limited).With(s.x.Galleries).All(ctx, tx); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "exceed the shared loading budget") {
			t.Fatal("two gallery files exceeded a one-value relation budget", err)
		}
		if _, err := articles.QueryArticles().With(s.x.Title, s.x.Title).All(ctx, tx); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "registered more than once") {
			t.Fatal("a repeated slot was accepted", err)
		}
		shallow := query.RelationLimits{BatchSize: 500, MaxRows: 100, MaxDepth: 1}
		if _, err := articles.QueryArticleAuthors().WithRelationLimits(shallow).With(articles.AuthorRelations().Articles.With(s.x.Title)).All(ctx, tx); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "depth bound") {
			t.Fatal("a slot nested beyond MaxDepth was accepted", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := articles.QueryArticles().With(s.x.Title).Load(cancelled, tx, []articles.Article{s.full}); !errors.Is(err, context.Canceled) {
			t.Fatal("a cancelled slot load ran", err)
		}
		return nil
	})
}

// A slot loaded through the writing transaction sees that transaction's own
// uncommitted writes; the store's separate snapshot cannot.
func TestSlotLoadingReadsTheCallersTransaction(t *testing.T) {
	s := seed(t)
	runtime, err := s.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("discard draft")
	err = s.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if err := s.x.Title.Field().SetIn(ctx, tx, runtime.Translations, s.empty.FoundryReference(), "en", "Draft"); err != nil {
			return err
		}
		joined, err := articles.QueryArticles().With(s.x.Title).RequireFind(ctx, tx, s.empty.ID)
		if err != nil {
			return err
		}
		if text, err := joined.Title.Exact("en"); err != nil || text.IsZero() {
			t.Fatal("the joined load missed its own write", err)
		} else if draft, _ := text.Get(); draft != "Draft" {
			t.Fatal("the joined load missed its own write", err)
		}
		separate, err := articles.QueryArticles().With(s.x.Title).RequireFind(ctx, &queryfixture.QueryCounter{Executor: tx}, s.empty.ID)
		if err != nil {
			return err
		}
		if text, err := separate.Title.Exact("en"); err != nil || text.IsSet() {
			t.Fatal("the store snapshot saw an uncommitted write", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}

func galleryAttachment(t *testing.T, s seeded, runtime slots.Runtime, id attachments.ID[articles.Article]) attachments.Attachment[articles.Article, model.ID[articles.Article]] {
	t.Helper()
	file, err := s.x.Galleries.Collection().Find(t.Context(), runtime.Attachments, s.full.FoundryReference(), id)
	if err != nil {
		t.Fatal(err)
	}
	return file
}
