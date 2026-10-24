// Package extensions exercises declaration-owned extension slots without model generation.
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
type Article struct {
	ID      int64
	Title   translations.Text
	Cover   attachments.One[Article]
	Gallery attachments.Many[Article]
	SEO     metadata.Value[string]
}

// Author supplies an unrelated owner for compiler fixtures. Example: Author{ID:1}.
type Author struct{ ID int64 }

// ArticleTable binds persisted and extension fields. Example: Articles.Query().
type ArticleTable struct {
	// Table owns the primary key. Example: Articles.Key().Eq(1).
	orm.Table[Article, int64]
	// Title binds translated text. Example: extensions.BindSlot(Articles.Key(),Articles.Title).
	Title orm.Slot[Article, translations.Text]
	// Cover binds one attachment. Example: extensions.BindSlot(Articles.Key(),Articles.Cover).
	Cover orm.Slot[Article, attachments.One[Article]]
	// Gallery binds multiple attachments. Example: extensions.BindSlot(Articles.Key(),Articles.Gallery).
	Gallery orm.Slot[Article, attachments.Many[Article]]
	// SEO binds a concrete metadata value. Example: extensions.BindSlot(Articles.Key(),Articles.SEO).
	SEO orm.Slot[Article, metadata.Value[string]]
}

// Articles declares the persisted owner. Example: Articles.Find(ctx,db,1).
var Articles = orm.Define("extension_articles", func(b orm.Builder[Article], m *Article) ArticleTable {
	return ArticleTable{orm.TableOf(b, &m.ID), orm.SlotOf(b, &m.Title), orm.SlotOf(b, &m.Cover), orm.SlotOf(b, &m.Gallery), orm.SlotOf(b, &m.SEO)}
})

// Authors is the unrelated declaration. Example: Authors.Find(ctx,db,1).
var Authors = orm.Define("extension_authors", func(b orm.Builder[Author], m *Author) orm.Table[Author, int64] { return orm.TableOf(b, &m.ID) })

// Owner pins storage identity independently of future table renames. Example: Owner.Reference(1).
var Owner = extensions.DefineORMOwner("articles", orm.IdentityOf(Articles.Table, Articles.Key()), extensions.OwnerOptions{StorageModel: "extension_articles"})

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
