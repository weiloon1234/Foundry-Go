package articles_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Registering the generated declaration is enough for hard-delete cleanup: no
// application hook composes metadata, translation or attachment cleanup.
func TestGeneratedDeclarationCleansExtensionDataOnHardDelete(t *testing.T) {
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Translations == nil || runtime.Attachments == nil || runtime.Metadata == nil {
		t.Fatal("configured runtime lacks an enabled manager")
	}
	if models := f.app.Models(); len(models) != 1 || models[0].Owner().Name() != "articles" {
		t.Fatal("the application does not report its registered model declarations", len(models))
	}
	x := articles.ArticleSlots.From(runtime)
	article := f.createArticle(t, "cleanup")
	if article.Title.IsLoaded() || article.Logo.IsLoaded() || article.SEO.IsLoaded() {
		t.Fatal("a written model's extension slots must start unloaded")
	}
	ref := articles.ArticleIdentity.ReferenceOf(article)
	if err := translations.FieldOf(x.Title).Set(t.Context(), runtime.Translations, ref, "en", "Hello"); err != nil {
		t.Fatal(err)
	}
	if err := metadata.KeyOf(x.SEO).Set(t.Context(), runtime.Metadata, ref, articles.SEO{Canonical: value.Set("/hello")}); err != nil {
		t.Fatal(err)
	}
	upload, err := attachments.OneCollection(x.Logo).Replace(t.Context(), runtime.Attachments, ref, attachments.Upload{Source: bytes.NewReader(pngImage(t, 40)), OriginalName: "logo.png"})
	if err != nil || upload.Publication != attachments.Published {
		t.Fatal("logo publication", err)
	}
	logo, _ := upload.Attachment.Get()
	write := func(run func(ctx context.Context, tx *database.Tx) error) error {
		return f.store.Write(t.Context(), run)
	}

	// Soft deletion keeps extension data for restoration.
	if err := write(func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Delete(ctx, tx, articles.Articles.Table.Query(), articles.Articles.Table.Key(), article.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := translations.FieldOf(x.Title).Get(t.Context(), runtime.Translations, ref, "en"); !errors.Is(err, database.NotFound) {
		t.Fatal("soft-deleted owner remained readable", err)
	}
	if err := write(func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Restore(ctx, tx, articles.Articles.Table, article.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	text, err := translations.FieldOf(x.Title).Get(t.Context(), runtime.Translations, ref, "en")
	if restored, ok := text.Get(); err != nil || !ok || restored != "Hello" {
		t.Fatal("restored translation lost", err)
	}

	// A rolled-back hard deletion rolls back its cleanup and keeps the file.
	rollback := errors.New("rollback deletion")
	if err := write(func(ctx context.Context, tx *database.Tx) error {
		if _, err := orm.ForceDelete(ctx, tx, articles.Articles.Table.Query(), articles.Articles.Soft, articles.Articles.Table.Key(), article.ID); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err := attachments.OneCollection(x.Logo).Find(t.Context(), runtime.Attachments, ref, logo.ID()); err != nil {
		t.Fatal("parent rollback lost the logo", err)
	}
	if stored, err := metadata.KeyOf(x.SEO).Get(t.Context(), runtime.Metadata, ref); err != nil || !stored.IsSet() {
		t.Fatal("parent rollback lost metadata", err)
	}

	// The committed hard deletion removes rows and schedules file cleanup.
	if err := write(func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.ForceDelete(ctx, tx, articles.Articles.Table.Query(), articles.Articles.Soft, articles.Articles.Table.Key(), article.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if state, err := runtime.Attachments.Inspect(t.Context(), upload.Operation); err != nil || state.State != attachments.Cleaned {
		t.Fatal("generated hard-delete observer kept the logo", err)
	}
	owner := articles.ArticleOwner
	if page, err := metadata.InspectOrphans(t.Context(), runtime.Metadata, owner.Name(), metadata.Cursor{}, 10); err != nil || len(page.Orphans) != 0 {
		t.Fatal("metadata rows survived hard deletion", err)
	}
	if page, err := translations.InspectOrphans(t.Context(), runtime.Translations, owner.Name(), translations.Cursor{}, 10); err != nil || len(page.Orphans) != 0 {
		t.Fatal("translation rows survived hard deletion", err)
	}

	// A manually composed cleanup, kept from before adopting slots, is harmless.
	if err := write(func(ctx context.Context, tx *database.Tx) error {
		if err := translations.Cleanup(ctx, tx, runtime.Translations, owner, ref, lifecycle.ForceDelete); err != nil {
			return err
		}
		return metadata.Cleanup(ctx, tx, runtime.Metadata, owner, ref, lifecycle.ForceDelete)
	}); err != nil {
		t.Fatal("duplicate cleanup after deletion failed", err)
	}
	// Identity-based cleanup, as a durable cleanup job runs it, is harmless too.
	identity, err := ref.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := write(func(ctx context.Context, tx *database.Tx) error {
		if err := metadata.CleanupIdentity(ctx, tx, runtime.Metadata, owner.Name(), identity); err != nil {
			return err
		}
		if err := translations.CleanupIdentity(ctx, tx, runtime.Translations, owner.Name(), identity); err != nil {
			return err
		}
		return attachments.CleanupIdentity(ctx, tx, runtime.Attachments, owner.Name(), identity, nil)
	}); err != nil {
		t.Fatal("identity cleanup after deletion failed", err)
	}
}

// A model deleted through another configured pool cannot share the store's
// transaction. The observer is registered on every pool; cleanup runs after the
// deletion commits, skips an owner that exists again, and a rolled-back
// deletion schedules nothing.
func TestCleanupAfterDeletionThroughAnotherPool(t *testing.T) {
	f := startApplication(t, withWriter)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.services.Databases.Connection("writer")
	if err != nil || writer == f.store.Database() {
		t.Fatal("the writer pool must differ from the extension store's", err)
	}
	x := articles.ArticleSlots.From(runtime)
	article := f.createArticle(t, "cross-pool")
	ref := articles.ArticleIdentity.ReferenceOf(article)
	if err := translations.FieldOf(x.Title).Set(t.Context(), runtime.Translations, ref, "en", "Hello"); err != nil {
		t.Fatal(err)
	}
	if err := metadata.KeyOf(x.SEO).Set(t.Context(), runtime.Metadata, ref, articles.SEO{Canonical: value.Set("/hello")}); err != nil {
		t.Fatal(err)
	}
	upload, err := attachments.OneCollection(x.Logo).Replace(t.Context(), runtime.Attachments, ref, attachments.Upload{Source: bytes.NewReader(pngImage(t, 40)), OriginalName: "logo.png"})
	if err != nil || upload.Publication != attachments.Published {
		t.Fatal("logo publication", err)
	}
	logo, _ := upload.Attachment.Get()
	kept := func(stage string) {
		t.Helper()
		if _, err := attachments.OneCollection(x.Logo).Find(t.Context(), runtime.Attachments, ref, logo.ID()); err != nil {
			t.Fatal(stage, "lost the logo", err)
		}
		if stored, err := metadata.KeyOf(x.SEO).Get(t.Context(), runtime.Metadata, ref); err != nil || !stored.IsSet() {
			t.Fatal(stage, "lost metadata", err)
		}
	}

	// The cleanup observer observes only deletions, so on every connection a
	// set-based update needs no WithoutModelHooks while a set-based deletion,
	// which would skip cleanup, still requires the acknowledgement.
	only := articles.Articles.Query().Where(orm.KeyEq(articles.Articles.Key(), article.ID))
	if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.UpdateAll(ctx, tx, only, orm.TextSet(articles.Articles.Slug, article.Slug))
		return err
	}); err != nil {
		t.Fatal("a set-based update was refused because of the deletion observer", err)
	}
	for name, remove := range map[string]func(context.Context, *database.Tx) (int64, error){
		"soft": func(ctx context.Context, tx *database.Tx) (int64, error) { return orm.DeleteAll(ctx, tx, only) },
		"force": func(ctx context.Context, tx *database.Tx) (int64, error) {
			return orm.ForceDeleteAll(ctx, tx, only, articles.Articles.Soft)
		},
	} {
		if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error { _, err := remove(ctx, tx); return err }); !errors.Is(err, fault.Invalid) {
			t.Fatal(name+" set-based deletion skipped cleanup without acknowledgement", err)
		}
	}
	rollback := errors.New("rollback deletion")
	if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error {
		if _, err := orm.ForceDelete(ctx, tx, articles.Articles.Table.Query(), articles.Articles.Soft, articles.Articles.Table.Key(), article.ID); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	kept("a rolled-back deletion")

	// Deleted and created again before commit: the owner exists, so nothing is removed.
	if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error {
		if _, err := orm.ForceDelete(ctx, tx, articles.Articles.Table.Query(), articles.Articles.Soft, articles.Articles.Table.Key(), article.ID); err != nil {
			return err
		}
		_, err := orm.Insert(ctx, tx, articles.Articles.Table, orm.KeySet(articles.Articles.Key(), article.ID), orm.TextSet(articles.Articles.Slug, article.Slug), orm.ColumnSet(articles.Articles.AuthorID, article.AuthorID))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	kept("a re-created owner")

	if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.ForceDelete(ctx, tx, articles.Articles.Table.Query(), articles.Articles.Soft, articles.Articles.Table.Key(), article.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if state, err := runtime.Attachments.Inspect(t.Context(), upload.Operation); err != nil || state.State != attachments.Cleaned {
		t.Fatal("cleanup after the committed deletion kept the logo", err)
	}
	owner := articles.ArticleOwner
	if page, err := metadata.InspectOrphans(t.Context(), runtime.Metadata, owner.Name(), metadata.Cursor{}, 10); err != nil || len(page.Orphans) != 0 {
		t.Fatal("metadata rows survived the deletion", err)
	}
	if page, err := translations.InspectOrphans(t.Context(), runtime.Translations, owner.Name(), translations.Cursor{}, 10); err != nil || len(page.Orphans) != 0 {
		t.Fatal("translation rows survived the deletion", err)
	}
}

// Cleanup after a deletion through another pool admits each store step on its
// own, as cleanup inside a deletion transaction does, so it succeeds even when
// the extension store admits a single operation at a time.
func TestCleanupThroughAnotherPoolWithOneStoreSlot(t *testing.T) {
	f := startApplication(t, withWriter, func(s *application.Settings) { s.Features.Extensions.MaxActive = 1 })
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	x := articles.ArticleSlots.From(runtime)
	article := f.createArticle(t, "one-slot")
	if err := metadata.KeyOf(x.SEO).Set(t.Context(), runtime.Metadata, articles.ArticleIdentity.ReferenceOf(article), articles.SEO{Canonical: value.Set("/one-slot")}); err != nil {
		t.Fatal(err)
	}
	if err := f.throughWriter(t, func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.ForceDelete(ctx, tx, articles.Articles.Table.Query(), articles.Articles.Soft, articles.Articles.Table.Key(), article.ID)
		return err
	}); err != nil {
		t.Fatal("cleanup after the deletion failed with one store slot", err)
	}
	if page, err := metadata.InspectOrphans(t.Context(), runtime.Metadata, articles.ArticleOwner.Name(), metadata.Cursor{}, 10); err != nil || len(page.Orphans) != 0 {
		t.Fatal("metadata rows survived the deletion", err)
	}
}
