package articles_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/value"
)

// memoryFile is a FileSource like foundryhttp.UploadedFile.
type memoryFile struct {
	name string
	data []byte
}

func (f memoryFile) Open(context.Context) (io.ReadSeekCloser, error) {
	return nopCloser{bytes.NewReader(f.data)}, nil
}
func (f memoryFile) Name() string              { return f.name }
func (f memoryFile) ClientContentType() string { return "image/png; charset=binary" }

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

// Slot writes take the model value, join the caller's transaction and keep
// the translation, metadata and attachment semantics of their stores.
func TestSlotWrites(t *testing.T) {
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleExtensions().From(runtime)
	article := f.createArticle(t, "writes")
	title := func() map[i18n.LocaleID]string {
		t.Helper()
		var loaded articles.Article
		if err := f.store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			var err error
			loaded, err = articles.QueryArticles().With(x.Title, x.SEO).RequireFind(ctx, tx, article.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		values, loadedTitle := loaded.Title.Values()
		if !loadedTitle {
			t.Fatal("title was not loaded")
		}
		return values.Entries()
	}
	if err := x.Title.Save(t.Context(), article, map[i18n.LocaleID]string{"en": "Hello", "ms": "Helo"}); err != nil {
		t.Fatal(err)
	}
	// Merge leaves unmentioned locales; empty text is a present value.
	if err := x.Title.Save(t.Context(), article, map[i18n.LocaleID]string{"zh": ""}); err != nil {
		t.Fatal(err)
	}
	if got := title(); len(got) != 3 || got["ms"] != "Helo" || got["zh"] != "" {
		t.Fatal("merge", got)
	}
	// Synchronization removes locales absent from the input and enforces the
	// declared default-locale requirement.
	if err := x.Title.Sync(t.Context(), article, map[i18n.LocaleID]string{"ms": "Helo"}); !errors.Is(err, fault.Invalid) {
		t.Fatal("sync without the required default locale", err)
	}
	if err := x.Title.Sync(t.Context(), article, map[i18n.LocaleID]string{"en": "Only"}); err != nil {
		t.Fatal(err)
	}
	if got := title(); len(got) != 1 || got["en"] != "Only" {
		t.Fatal("sync", got)
	}
	if err := x.Title.Save(t.Context(), article, map[i18n.LocaleID]string{"fr": "Bonjour"}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsupported locale was written", err)
	}

	// A parent rollback rolls back text and metadata written inside it.
	rollback := errors.New("rollback")
	err = f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if err := x.Title.SaveIn(ctx, tx, article, map[i18n.LocaleID]string{"en": "Draft"}); err != nil {
			return err
		}
		if err := x.SEO.SaveIn(ctx, tx, article, articles.SEO{Canonical: value.Set("/draft")}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if got := title(); got["en"] != "Only" {
		t.Fatal("rolled-back text survived", got)
	}
	if stored, err := x.SEO.Key().Get(t.Context(), runtime.Metadata, article.FoundryReference()); err != nil || stored.IsSet() {
		t.Fatal("rolled-back metadata survived", err)
	}
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if err := x.SEO.SaveIn(ctx, tx, article, articles.SEO{Canonical: value.Set("/kept")}); err != nil {
			return err
		}
		if removed, err := x.Title.ForgetIn(ctx, tx, article, "en"); err != nil || !removed {
			t.Fatal("forget", removed, err)
		}
		if count, err := x.Summary.ClearIn(ctx, tx, article); err != nil || count != 0 {
			t.Fatal("clear", count, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := title(); len(got) != 0 {
		t.Fatal("forgotten locale survived", got)
	}
	if removed, err := x.SEO.Forget(t.Context(), article); err != nil || !removed {
		t.Fatal("metadata forget", removed, err)
	}

	// Writes through unbound descriptors fail instead of finding services.
	unbound := articles.ArticleExtensions()
	if err := unbound.Title.Save(t.Context(), article, map[i18n.LocaleID]string{"en": "x"}); !errors.Is(err, fault.Missing) {
		t.Fatal("unbound translated write", err)
	}
	if _, err := unbound.Logo.ReplaceFile(t.Context(), article, memoryFile{"logo.png", pngImage(t, 40)}); !errors.Is(err, fault.Missing) {
		t.Fatal("unbound attachment write", err)
	}

	// Files open, publish and close within the call; the client type is only
	// a hint, and Accepts matches write-time detection.
	if ok, err := x.Logo.Accepts(t.Context(), memoryFile{"logo.png", pngImage(t, 40)}); err != nil || !ok {
		t.Fatal("a PNG logo was rejected", err)
	}
	if ok, err := x.Logo.Accepts(t.Context(), memoryFile{"logo.png", []byte("not an image")}); err != nil || ok {
		t.Fatal("text was accepted as a PNG logo", err)
	}
	result, err := x.Logo.ReplaceFile(t.Context(), article, memoryFile{"logo.png", pngImage(t, 40)})
	if err != nil || result.Publication != attachments.Published {
		t.Fatal("logo publication", err)
	}
	gallery := []memoryFile{{"a.png", pngImage(t, 8)}, {"b.png", pngImage(t, 8)}, {"c.png", pngImage(t, 8)}}
	if results, err := attachments.AddFiles(t.Context(), x.Galleries, article, gallery); err != nil || len(results) != 3 {
		t.Fatal("gallery publication", err)
	}
	// The collection holds four files: the fifth file fails after the fourth
	// published, and both attempts are reported.
	results, err := attachments.AddFiles(t.Context(), x.Galleries, article, []memoryFile{{"d.png", pngImage(t, 8)}, {"e.png", pngImage(t, 8)}})
	if err == nil || len(results) != 2 || results[0].Publication != attachments.Published || results[1].Publication != attachments.Unpublished {
		t.Fatal("partial gallery publication was not reported", len(results), err)
	}
}

// A stored metadata version different from the slot's declaration fails the
// load instead of decoding it under the wrong contract.
func TestSlotMetadataVersionMismatchFailsTheLoad(t *testing.T) {
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleExtensions().From(runtime)
	article := f.createArticle(t, "versioned")
	newer := metadata.Define(articles.ArticleExtensionOwner(), "seo", 2, articles.SEOJSON())
	manager, err := metadata.New(f.store, newer.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := newer.Set(t.Context(), manager, article.FoundryReference(), articles.SEO{Canonical: value.Set("/v2")}); err != nil {
		t.Fatal(err)
	}
	err = f.store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := articles.QueryArticles().With(x.SEO).RequireFind(ctx, tx, article.ID)
		return err
	})
	if !errors.Is(err, fault.Conflict) {
		t.Fatal("a newer stored version decoded under the older slot contract", err)
	}
}
