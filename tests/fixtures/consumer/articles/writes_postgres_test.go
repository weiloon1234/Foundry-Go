package articles_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
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

// A transaction that publishes attachments needs only its own connection:
// PrepareFile stores the file before it begins, and ReplaceIn or AddIn publish
// inside it. The whole test runs on a one-connection pool, so any second
// connection taken while the transaction is open would time out.
func TestSlotAttachmentsPublishInsideTheCallersTransaction(t *testing.T) {
	f := startApplication(t, func(s *application.Settings) {
		single := s.Services.Database.Connections["default"]
		single.Primary.Pool.MaxOpen, single.Primary.Pool.MaxIdle, single.Primary.Pool.AcquireTimeout = 1, 1, 2*time.Second
		s.Services.Database.Connections["default"] = single
		s.Services.Database.Connections["writer"] = single
	})
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleExtensions().From(runtime)
	logo, gallery := memoryFile{name: "logo.png", data: pngImage(t, 40)}, memoryFile{name: "gallery.png", data: pngImage(t, 24)}
	state := func(operation attachments.OperationID) attachments.State {
		t.Helper()
		status, err := runtime.Attachments.Inspect(t.Context(), operation)
		if err != nil {
			t.Fatal(err)
		}
		return status.State
	}
	prepare := func(owner articles.Article, file memoryFile) attachments.Prepared[articles.Article, model.ID[articles.Article]] {
		t.Helper()
		prepared, err := x.Logo.PrepareFile(t.Context(), owner, file)
		if err != nil || prepared.IsZero() || state(prepared.Operation()) != attachments.Stored {
			t.Fatal("prepare", err)
		}
		return prepared
	}

	// A model created in the transaction owns files prepared for its chosen key.
	id, err := model.NewID[articles.Article]()
	if err != nil {
		t.Fatal(err)
	}
	first := prepare(articles.Article{ID: id}, logo)
	pictures, err := x.Galleries.PrepareFile(t.Context(), articles.Article{ID: id}, gallery)
	if err != nil {
		t.Fatal(err)
	}
	var article articles.Article
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		author, err := articles.QueryArticleAuthors().Create(ctx, tx, articles.AuthorDraft{}.SetName("Author in transaction"))
		if err != nil {
			return err
		}
		if article, err = articles.QueryArticles().Create(ctx, tx, articles.ArticleDraft{}.SetID(id).SetSlug("in-transaction").SetAuthorID(author.ID)); err != nil {
			return err
		}
		if result, err := x.Logo.ReplaceIn(ctx, tx, article, first); err != nil || result.Publication != attachments.Published {
			return errors.Join(errors.New("logo was not published"), err)
		}
		// A prepared upload belongs to the collection that prepared it.
		if _, err := x.Galleries.AddIn(ctx, tx, article, first); !errors.Is(err, fault.Invalid) {
			return errors.Join(errors.New("an upload of another collection was published"), err)
		}
		_, err = x.Galleries.AddIn(ctx, tx, article, pictures)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var loaded articles.Article
	if err := f.store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		var err error
		loaded, err = articles.QueryArticles().With(x.Logo, x.Galleries).RequireFind(ctx, tx, article.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if file, ok := loaded.Logo.Get(); !ok || !file.IsSet() || loaded.Galleries.Len() != 1 {
		t.Fatal("files published in the committed transaction are not loaded")
	}

	// A rollback unpublishes; the prepared upload stays stored and can be
	// published again, and replacing cleans the old file after commit.
	second := prepare(article, logo)
	rollback := errors.New("rollback")
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := x.Logo.ReplaceIn(ctx, tx, article, second); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) || state(second.Operation()) != attachments.Stored {
		t.Fatal("a rolled-back publication must leave the prepared upload stored", err)
	}
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		result, err := x.Logo.ReplaceIn(ctx, tx, article, second)
		if err == nil && (len(result.PendingCleanup) != 1 || result.PendingCleanup[0] != first.Operation()) {
			err = errors.New("the replaced file was not reported for cleanup")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if state(first.Operation()) != attachments.Cleaned || state(second.Operation()) != attachments.Ready {
		t.Fatal("the replaced file was not cleaned after commit")
	}

	// An owner the transaction cannot see fails publication; Discard reclaims
	// the upload at once, and a published one is kept.
	missing, err := model.NewID[articles.Article]()
	if err != nil {
		t.Fatal(err)
	}
	orphan := prepare(articles.Article{ID: missing}, logo)
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := x.Logo.ReplaceIn(ctx, tx, articles.Article{ID: missing}, orphan)
		return err
	}); !errors.Is(err, database.NotFound) {
		t.Fatal("publication for a missing owner", err)
	}
	if err := x.Logo.Discard(t.Context(), orphan); err != nil || state(orphan.Operation()) != attachments.Cleaned {
		t.Fatal("discard", err)
	}
	if err := x.Logo.Discard(t.Context(), second); err == nil || state(second.Operation()) != attachments.Ready {
		t.Fatal("discard removed a published upload", err)
	}

	// A prepared upload abandoned before publication is reclaimed after
	// StoredGrace by reconciliation.
	abandoned := prepare(article, logo)
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET updated_at = updated_at - interval '2 hours' WHERE state = $1`, string(attachments.Stored))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Attachments.ReconcilePending(t.Context(), 10); err != nil || state(abandoned.Operation()) != attachments.Cleaned {
		t.Fatal("reconciliation kept the abandoned upload", err)
	}

	// Any isolation sees an upload prepared before the transaction began.
	visible := prepare(article, logo)
	if err := f.store.Database().Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := x.Logo.ReplaceIn(t.Context(), tx, article, visible)
		return err
	}, database.TxOptions{Isolation: database.RepeatableRead}); err != nil || state(visible.Operation()) != attachments.Ready {
		t.Fatal("a REPEATABLE READ transaction could not publish an upload prepared before it", err)
	}
	writer, err := f.services.Databases.Connection("writer")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := x.Logo.ReplaceIn(t.Context(), tx, article, visible)
		if !errors.Is(err, fault.Invalid) {
			return errors.Join(errors.New("a foreign transaction published an upload"), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Logo.ReplaceIn(t.Context(), nil, article, visible); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil transaction accepted", err)
	}
}

// An upload prepared after a REPEATABLE READ transaction took its snapshot is
// invisible to that transaction, which reports it as such rather than as a
// missing owner. Prepare uploads before the transaction begins.
func TestPreparedUploadAfterTheSnapshotIsReportedInvisible(t *testing.T) {
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleExtensions().From(runtime)
	article := f.createArticle(t, "snapshot")
	if err := f.store.Database().Transaction(t.Context(), func(tx *database.Tx) error {
		var one int
		if err := database.ScanOne(t.Context(), tx, `SELECT 1`, nil, &one); err != nil {
			return err
		}
		late, err := x.Logo.PrepareFile(t.Context(), article, memoryFile{name: "late.png", data: pngImage(t, 40)})
		if err != nil {
			return err
		}
		if _, err := x.Logo.ReplaceIn(t.Context(), tx, article, late); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "not visible") {
			return errors.Join(errors.New("an upload prepared after the snapshot was not reported invisible"), err)
		}
		return nil
	}, database.TxOptions{Isolation: database.RepeatableRead}); err != nil {
		t.Fatal(err)
	}
}

// Prepare locks an existing owner while it records the upload's intent, so
// concurrent uploads for one owner are counted one at a time and cannot
// overshoot the owner's intent bound.
func TestPrepareLocksAnExistingOwner(t *testing.T) {
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleExtensions().From(runtime)
	article := f.createArticle(t, "locked")
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := articles.QueryArticles().ForUpdate().RequireFind(ctx, tx, article.ID); err != nil {
			return err
		}
		waiting, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		if _, err := x.Logo.PrepareFile(waiting, article, memoryFile{name: "logo.png", data: pngImage(t, 40)}); !errors.Is(err, context.DeadlineExceeded) {
			return errors.Join(errors.New("prepare did not wait for the owner lock"), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
