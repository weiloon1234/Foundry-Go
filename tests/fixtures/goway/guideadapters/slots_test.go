package guideadapters_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"

	ext "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// guide:owner-rename
// doc-example:begin model-extensions-account-owner
var AccountOwner = extensions.DefineORMOwner("accounts", AccountIdentity,
	extensions.OwnerOptions{StorageModel: "original_accounts"}) // doc-example:end model-extensions-account-owner

// guide:end

// guide:owner-recovery
// doc-example:begin model-extensions-recover-account-owner
var RecoverAccountOwner = extensions.DefineORMOwner("accounts", AccountIdentity,
	extensions.OwnerOptions{PreviousModels: []string{"original_accounts"}}) // doc-example:end model-extensions-recover-account-owner

// guide:end

// guide:slot-load
// doc-example:begin model-extension-slots-articles
func articles(ctx context.Context, db database.Executor, runtime slots.Runtime) ([]ext.Article, error) {
	bound := ArticleExtensions.From(runtime)
	return orm.All(ctx, db, ext.Articles.Query().With(bound.All()...))
} // doc-example:end model-extension-slots-articles

// guide:end

// guide:slot-write
// doc-example:begin model-extension-slots-save-article
func saveArticle(ctx context.Context, tx *database.Tx, bound ext.Descriptors, article ext.Article, title map[i18n.LocaleID]string, seo string) error {
	if err := bound.Title.SyncIn(ctx, tx, article, title); err != nil {
		return err
	}
	return bound.SEO.SaveIn(ctx, tx, article, seo)
} // doc-example:end model-extension-slots-save-article

// guide:end

// guide:variants
// doc-example:begin attachments-thumbnail
var Thumbnail = attachments.DefineVariant("thumbnail",
	imaging.NewPlan().Fit(320, 320, false).Format(imaging.WebP))
var Photos = attachments.Define(ext.Owner, "photos", attachments.Policy{
	Disk: storage.DefineDisk("photos"), Cardinality: attachments.Multiple,
	Accepted: []storage.MediaType{"image/jpeg", "image/png"},
	Variants: []attachments.Variant{Thumbnail},
}) // doc-example:end attachments-thumbnail

// guide:end

// guide:attachment-outbox
// doc-example:begin attachments-queued-photos
func queuedPhotos(producer *jobs.Outbox) (attachments.Collection[ext.Article, int64], error) {
	job := attachments.DefineReconcileJob("attachments.reconcile", jobs.DefaultPolicy("maintenance"))
	queue, err := job.ToOutbox(producer)
	if err != nil {
		return attachments.Collection[ext.Article, int64]{}, err
	}
	return Photos.WithReconciliation(queue), nil
} // doc-example:end attachments-queued-photos

// guide:end

// guide:slot-policies
// doc-example:begin model-extension-slots-article-extensions
// ArticleExtensions binds policies to the exact stored slot types.
// Example: ArticleExtensions.From(runtime).
var ArticleExtensions = ext.Descriptors{
	Title:   translations.TextOf(ext.Owner, "title", translations.Options{}, extensions.BindSlot(ext.Articles.Key(), ext.Articles.Title)),
	Cover:   attachments.OneOf(ext.Owner, "cover", attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}}, extensions.BindSlot(ext.Articles.Key(), ext.Articles.Cover)),
	Gallery: attachments.ManyOf(ext.Owner, "gallery", attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}}, extensions.BindSlot(ext.Articles.Key(), ext.Articles.Gallery)),
	SEO:     metadata.ValueOf(ext.Owner, "seo", metadata.Spec[string]{}, contract.StringJSON[string](), extensions.BindSlot(ext.Articles.Key(), ext.Articles.SEO)),
} // doc-example:end model-extension-slots-article-extensions

// guide:end

// guide:slot-declaration
// doc-example:begin model-extension-slots-article-declaration
func articleDeclaration() slots.Declaration {
	return slots.DeclareORM(ext.Owner, slots.Parts{
		Translations: []translations.Registration{ArticleExtensions.Title.Registration()},
		Attachments:  []attachments.Registration{ArticleExtensions.Cover.Registration(), ArticleExtensions.Gallery.Registration()},
		Metadata:     []metadata.Registration{ArticleExtensions.SEO.Registration()},
		Slots:        []extensions.SlotDescription{ArticleExtensions.Title.Describe(), ArticleExtensions.Cover.Describe(), ArticleExtensions.Gallery.Describe(), ArticleExtensions.SEO.Describe()},
	})
} // doc-example:end model-extension-slots-article-declaration

// guide:end
