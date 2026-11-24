// Package extensions exercises declaration-owned extension slots with generated model tables.
package extensions

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// Article is a methodless extension owner. Example: Article{ID:1}.
// doc-example:begin model-extension-slots-article
//
//foundry:model table=extension_articles var=Articles key=ID identity=false
type Article struct {
	// Table owns the primary key. Example: orm.Eq(Articles.Key(), 1).
	ID int64
	// Title binds translated text. Example: extensions.BindSlot(Articles.Key(),Articles.Title).
	Title translations.Text
	// Cover binds one attachment. Example: extensions.BindSlot(Articles.Key(),Articles.Cover).
	Cover attachments.One[Article]
	// Gallery binds multiple attachments. Example: extensions.BindSlot(Articles.Key(),Articles.Gallery).
	Gallery attachments.Many[Article]
	// SEO binds a concrete metadata value. Example: extensions.BindSlot(Articles.Key(),Articles.SEO).
	SEO metadata.Value[string]
} // doc-example:end model-extension-slots-article

// Author supplies an unrelated owner for compiler fixtures. Example: Author{ID:1}.
//
//foundry:model table=extension_authors var=Authors key=ID identity=false
type Author struct{ ID int64 }

// Owner pins storage identity independently of future table renames. Example: extensions.ORMIdentity(Owner).Reference(1).
// doc-example:begin model-extension-slots-owner
var Owner = extensions.DefineORMOwner("articles", orm.IdentityOf(Articles.Table, Articles.Key()), extensions.OwnerOptions{StorageModel: "extension_articles"}) // doc-example:end model-extension-slots-owner

// Descriptors groups concrete extension capabilities. Example: ArticleExtensions.From(runtime).
type Descriptors struct {
	// Title retains the text policy and translation manager. Example: ArticleExtensions.Title.Rule().
	Title translations.TextHandle[Article, int64]
	// Cover retains single-file capability. Example: ArticleExtensions.Cover.From(manager).
	Cover attachments.OneHandle[Article, int64]
	// Gallery retains multi-file capability. Example: ArticleExtensions.Gallery.From(manager).
	Gallery attachments.ManyHandle[Article, int64]
	// SEO retains string input. Example: ArticleExtensions.SEO.Save(ctx,article,"canonical").
	SEO metadata.ValueHandle[Article, int64, string]
}

// From borrows the exact runtime families once. Example: ArticleExtensions.From(runtime).
func (d Descriptors) From(runtime slots.Runtime) Descriptors {
	d.Title = d.Title.From(runtime.Translations)
	d.Cover = d.Cover.From(runtime.Attachments)
	d.Gallery = d.Gallery.From(runtime.Attachments)
	d.SEO = d.SEO.From(runtime.Metadata)
	return d
}

// ArticleExtensions declares policies separately from field offsets. Example: ArticleExtensions.From(runtime).
var ArticleExtensions = Descriptors{
	Title:   translations.TextOf(Owner, "title", translations.Options{}, extensions.BindSlot(Articles.Key(), Articles.Title)),
	Cover:   attachments.OneOf(Owner, "cover", attachments.Policy{Disk: storage.DefineDisk("local")}, extensions.BindSlot(Articles.Key(), Articles.Cover)),
	Gallery: attachments.ManyOf(Owner, "gallery", attachments.Policy{Disk: storage.DefineDisk("local")}, extensions.BindSlot(Articles.Key(), Articles.Gallery)),
	SEO:     metadata.ValueOf(Owner, "seo", metadata.Spec[string]{}, contract.StringJSON[string](), extensions.BindSlot(Articles.Key(), Articles.SEO)),
}

// All returns owner-typed loaders. Example: Articles.Query().With(bound.All()...).
func (d Descriptors) All() []orm.Relation[Article] {
	return []orm.Relation[Article]{d.Title.Relation(Articles.Title), d.Cover.Relation(Articles.Cover), d.Gallery.Relation(Articles.Gallery), d.SEO.Relation(Articles.SEO)}
}

// Declaration includes the ordinary deletion cleanup observer. Example: builder.Models(Declaration()).
func Declaration() slots.Declaration {
	return slots.DeclareORM(Owner, slots.Parts{Translations: []translations.Registration{ArticleExtensions.Title.Registration()}, Attachments: []attachments.Registration{ArticleExtensions.Cover.Registration(), ArticleExtensions.Gallery.Registration()}, Metadata: []metadata.Registration{ArticleExtensions.SEO.Registration()}, Slots: []extensions.SlotDescription{ArticleExtensions.Title.Describe(), ArticleExtensions.Cover.Describe(), ArticleExtensions.Gallery.Describe(), ArticleExtensions.SEO.Describe()}})
}
