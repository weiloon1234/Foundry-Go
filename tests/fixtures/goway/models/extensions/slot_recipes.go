package extensions

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// doc-example:begin model-extension-slots-article-extensions
// ArticleSlots binds policies to the exact stored slot types.
var ArticleSlots = Descriptors{
	Title:   translations.TextOf(Owner, "title", translations.Options{}, extensions.BindSlot(Articles.Key(), Articles.Title)),
	Cover:   attachments.OneOf(Owner, "cover", attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}}, extensions.BindSlot(Articles.Key(), Articles.Cover)),
	Gallery: attachments.ManyOf(Owner, "gallery", attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}}, extensions.BindSlot(Articles.Key(), Articles.Gallery)),
	SEO:     metadata.ValueOf(Owner, "seo", metadata.Spec[string]{}, contract.StringJSON[string](), extensions.BindSlot(Articles.Key(), Articles.SEO)),
}

// doc-example:end model-extension-slots-article-extensions

// doc-example:begin model-extension-slots-article-declaration
func ArticleDeclaration() slots.Declaration {
	return slots.DeclareORM(Owner, slots.Parts{
		Translations: []translations.Registration{ArticleSlots.Title.Registration()},
		Attachments:  []attachments.Registration{ArticleSlots.Cover.Registration(), ArticleSlots.Gallery.Registration()},
		Metadata:     []metadata.Registration{ArticleSlots.SEO.Registration()},
		Slots:        []extensions.SlotDescription{ArticleSlots.Title.Describe(), ArticleSlots.Cover.Describe(), ArticleSlots.Gallery.Describe(), ArticleSlots.SEO.Describe()},
	})
}

// doc-example:end model-extension-slots-article-declaration

// doc-example:begin model-extension-slots-articles
func LoadArticles(ctx context.Context, db database.Executor, runtime slots.Runtime) ([]Article, error) {
	bound := ArticleSlots.From(runtime)
	return orm.All(ctx, db, Articles.Query().With(bound.All()...))
}

// doc-example:end model-extension-slots-articles

// doc-example:begin model-extension-slots-save-article
func SaveArticle(ctx context.Context, tx *database.Tx, bound Descriptors, article Article, title map[i18n.LocaleID]string, seo string) error {
	if err := bound.Title.SyncIn(ctx, tx, article, title); err != nil {
		return err
	}
	return bound.SEO.SaveIn(ctx, tx, article, seo)
}

// doc-example:end model-extension-slots-save-article

// doc-example:begin attachments-thumbnail
var Thumbnail = attachments.DefineVariant("thumbnail",
	imaging.NewPlan().Fit(320, 320, false).Format(imaging.WebP))
var Photos = attachments.Define(Owner, "photos", attachments.Policy{
	Disk: storage.DefineDisk("photos"), Cardinality: attachments.Multiple,
	Accepted: []storage.MediaType{"image/jpeg", "image/png"},
	Variants: []attachments.Variant{Thumbnail},
})

// doc-example:end attachments-thumbnail

// doc-example:begin attachments-queued-photos
func QueuedPhotos(producer *jobs.Outbox) (attachments.Collection[Article, int64], error) {
	job := attachments.DefineReconcileJob("attachments.reconcile", jobs.DefaultPolicy("maintenance"))
	queue, err := job.ToOutbox(producer)
	if err != nil {
		return attachments.Collection[Article, int64]{}, err
	}
	return Photos.WithReconciliation(queue), nil
}

// doc-example:end attachments-queued-photos

// doc-example:begin model-extensions-renamed-owner
var RenamedOwner = extensions.DefineORMOwner("articles", orm.IdentityOf(Articles.Table, Articles.Key()),
	extensions.OwnerOptions{StorageModel: "original_articles"})

// doc-example:end model-extensions-renamed-owner

// doc-example:begin model-extensions-recovered-owner
var RecoveredOwner = extensions.DefineORMOwner("articles", orm.IdentityOf(Articles.Table, Articles.Key()),
	extensions.OwnerOptions{PreviousModels: []string{"original_articles"}})

// doc-example:end model-extensions-recovered-owner
