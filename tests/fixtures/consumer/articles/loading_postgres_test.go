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
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

// seeded is one application with a fully populated and an empty article.
type seeded struct {
	fixture
	x           articles.ArticleSlotHandles
	full, empty articles.Article
}

func seed(t *testing.T) seeded {
	t.Helper()
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleSlots.From(runtime)
	full, empty := f.createArticle(t, "a-full"), f.createArticle(t, "b-empty")
	ref := articles.ArticleIdentity.ReferenceOf(full)
	for locale, text := range map[i18n.LocaleID]string{"en": "Hello", "ms": "Helo"} {
		if err := translations.FieldOf(x.Title).Set(t.Context(), runtime.Translations, ref, locale, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := translations.FieldOf(x.Summary).Set(t.Context(), runtime.Translations, ref, "en", ""); err != nil {
		t.Fatal(err)
	}
	if err := metadata.KeyOf(x.SEO).Set(t.Context(), runtime.Metadata, ref, articles.SEO{Canonical: value.Set("/hello"), Keywords: value.Set([]string{"go"})}); err != nil {
		t.Fatal(err)
	}
	if _, err := attachments.OneCollection(x.Logo).Replace(t.Context(), runtime.Attachments, ref, attachments.Upload{Source: bytes.NewReader(pngImage(t, 40)), OriginalName: "logo.png"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.png", "second.png"} {
		if _, err := attachments.ManyCollection(x.Galleries).Add(t.Context(), runtime.Attachments, ref, attachments.Upload{Source: bytes.NewReader(pngImage(t, 24)), OriginalName: name}); err != nil {
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
	fields := articles.Articles
	for name, executor := range map[string]func(*database.Tx) database.Executor{
		"joined transaction": func(tx *database.Tx) database.Executor { return tx },
		"store snapshot":     func(tx *database.Tx) database.Executor { return &queryfixture.QueryCounter{Executor: tx} },
	} {
		t.Run(name, func(t *testing.T) {
			var list []articles.Article
			s.read(t, func(ctx context.Context, tx *database.Tx) error {
				var err error
				list, err = articles.Articles.Query().
					With(articles.ArticleAuthor, s.x.Title.Relation(articles.Articles.Title), s.x.Summary.Relation(articles.Articles.Summary), s.x.Logo.Relation(articles.Articles.Logo), s.x.Galleries.Relation(articles.Articles.Galleries), s.x.SEO.Relation(articles.Articles.SEO)).
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
		plain, err = articles.Articles.Query().Where(articles.Articles.Key().Eq(s.full.ID)).Require(ctx, tx)
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
		loaded, err := articles.Articles.Query().With(s.x.Logo.Relation(articles.Articles.Logo), s.x.Galleries.Relation(articles.Articles.Galleries)).OrderBy(articles.Articles.Slug.Asc()).All(ctx, tx)
		if err == nil {
			full, empty = loaded[0], loaded[1]
		}
		return err
	})
	stored, err := attachments.OneCollection(s.x.Logo).List(t.Context(), runtime.Attachments, articles.ArticleIdentity.ReferenceOf(full))
	if err != nil || len(stored) != 1 {
		t.Fatal("explicit list", err)
	}
	_, want := attachments.OneCollection(s.x.Logo).PublicURLOf(t.Context(), runtime.Attachments, stored[0])
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
	_, want = attachments.ManyCollection(s.x.Galleries).VariantPublicURLOf(t.Context(), runtime.Attachments, galleryAttachment(t, s, runtime, file[0].ID()), articles.Thumbnail)
	_, got = s.x.Galleries.VariantURL(t.Context(), full, file[0], articles.Thumbnail)
	if (want == nil) != (got == nil) || (want != nil && want.Error() != got.Error()) {
		t.Fatal("slot variant link differs from the collection link", want, got)
	}
}

// Relation loading paths reuse the slot descriptors: explicit Load and
// LoadMissing, pagination, chunking and nested relations.
func TestSlotsLoadThroughEveryRelationPath(t *testing.T) {
	s := seed(t)
	fields := articles.Articles
	s.read(t, func(ctx context.Context, tx *database.Tx) error {
		loaded, err := orm.Load(ctx, tx, []articles.Article{s.full, s.full}, s.x.Title.Relation(articles.Articles.Title))
		if err != nil {
			return err
		}
		if len(loaded) != 2 || !loaded[0].Title.IsLoaded() || !loaded[1].Title.IsLoaded() || s.full.Title.IsLoaded() {
			t.Fatal("explicit Load must fill copies, including repeated parents")
		}
		// LoadMissing's zero-statement case is measured with a statement
		// observer in TestSlotLoadingStatementsDoNotGrowWithParents.
		missing, err := orm.LoadMissing(ctx, tx, loaded, s.x.Title.Relation(articles.Articles.Title))
		if err != nil {
			return err
		}
		if !missing[0].Title.IsLoaded() {
			t.Fatal("LoadMissing dropped a loaded slot")
		}
		page, err := articles.Articles.Query().With(s.x.SEO.Relation(articles.Articles.SEO)).OrderBy(fields.Slug.Asc()).Paginate(ctx, tx, orm.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if len(page.Items) != 1 || !page.Items[0].SEO.IsLoaded() || page.Total != 2 {
			t.Fatal("paginated slot loading")
		}
		simple, err := articles.Articles.Query().With(s.x.SEO.Relation(articles.Articles.SEO)).OrderBy(fields.Slug.Asc()).SimplePaginate(ctx, tx, orm.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if len(simple.Items) != 1 || !simple.Items[0].SEO.IsLoaded() || !simple.HasMore {
			t.Fatal("simple paginated slot loading")
		}
		cursor, err := orm.CursorPaginate(ctx, tx, articles.Articles.Query().With(s.x.Title.Relation(articles.Articles.Title)).OrderBy(fields.Slug.Asc()), orm.CursorRequest[articles.Article]{Size: 1})
		if err != nil {
			return err
		}
		next, more := cursor.Next.Get()
		if len(cursor.Items) != 1 || !cursor.Items[0].Title.IsLoaded() || !more {
			t.Fatal("cursor paginated slot loading")
		}
		following, err := orm.CursorPaginate(ctx, tx, articles.Articles.Query().With(s.x.Title.Relation(articles.Articles.Title)).OrderBy(fields.Slug.Asc()), orm.CursorRequest[articles.Article]{Size: 1, After: value.Set(next)})
		if err != nil {
			return err
		}
		if len(following.Items) != 1 || !following.Items[0].Title.IsLoaded() || following.Items[0].ID == cursor.Items[0].ID {
			t.Fatal("second cursor page slot loading")
		}
		chunked := 0
		if err := articles.Articles.Query().With(s.x.Galleries.Relation(articles.Articles.Galleries)).EachChunked(ctx, tx, 1, func(article articles.Article) error {
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
		authors, err := articles.Authors.Query().With(articles.AuthorArticles.With(s.x.Title.Relation(articles.Articles.Title))).All(ctx, tx)
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
		_, err := articles.Articles.Delete(ctx, tx, s.empty.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.read(t, func(ctx context.Context, tx *database.Tx) error {
		trashed, err := articles.Articles.Query().OnlyTrashed().With(s.x.Title.Relation(articles.Articles.Title), s.x.Galleries.Relation(articles.Articles.Galleries)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(trashed) != 1 || trashed[0].Title.IsLoaded() || trashed[0].Galleries.IsLoaded() {
			t.Fatal("a soft-deleted owner's slots must stay unloaded")
		}
		if _, err := articles.Articles.Query().With(articles.ArticleSlots.Title.Relation(articles.Articles.Title)).All(ctx, tx); !errors.Is(err, fault.Missing) {
			t.Fatal("an unbound slot must fail before loading", err)
		}
		limited := orm.RelationLimits{BatchSize: 500, MaxRows: 1, MaxDepth: 8}
		if _, err := orm.WithRelationLimits(articles.Articles.Query(), limited).With(s.x.Galleries.Relation(articles.Articles.Galleries)).All(ctx, tx); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "exceed the shared loading budget") {
			t.Fatal("two gallery files exceeded a one-value relation budget", err)
		}
		if _, err := articles.Articles.Query().With(s.x.Title.Relation(articles.Articles.Title), s.x.Title.Relation(articles.Articles.Title)).All(ctx, tx); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "registered more than once") {
			t.Fatal("a repeated slot was accepted", err)
		}
		shallow := orm.RelationLimits{BatchSize: 500, MaxRows: 100, MaxDepth: 1}
		if _, err := orm.WithRelationLimits(articles.Authors.Query(), shallow).With(articles.AuthorArticles.With(s.x.Title.Relation(articles.Articles.Title))).All(ctx, tx); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "depth bound") {
			t.Fatal("a slot nested beyond MaxDepth was accepted", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := orm.Load(cancelled, tx, []articles.Article{s.full}, s.x.Title.Relation(articles.Articles.Title)); !errors.Is(err, context.Canceled) {
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
		if err := translations.FieldOf(s.x.Title).SetIn(ctx, tx, runtime.Translations, articles.ArticleIdentity.ReferenceOf(s.empty), "en", "Draft"); err != nil {
			return err
		}
		joined, err := articles.Articles.Query().With(s.x.Title.Relation(articles.Articles.Title)).Where(articles.Articles.Key().Eq(s.empty.ID)).Require(ctx, tx)
		if err != nil {
			return err
		}
		if text, err := joined.Title.Exact("en"); err != nil || text.IsZero() {
			t.Fatal("the joined load missed its own write", err)
		} else if draft, _ := text.Get(); draft != "Draft" {
			t.Fatal("the joined load missed its own write", err)
		}
		separate, err := articles.Articles.Query().With(s.x.Title.Relation(articles.Articles.Title)).Where(articles.Articles.Key().Eq(s.empty.ID)).Require(ctx, &queryfixture.QueryCounter{Executor: tx})
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
	file, err := attachments.ManyCollection(s.x.Galleries).Find(t.Context(), runtime.Attachments, articles.ArticleIdentity.ReferenceOf(s.full), id)
	if err != nil {
		t.Fatal(err)
	}
	return file
}
