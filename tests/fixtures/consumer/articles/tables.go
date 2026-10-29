package articles

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AuthorTable exposes stored author fields and the article slot. Example: Authors.Query().
type AuthorTable struct {
	// Table owns the concrete author key. Example: orm.KeyEq(Authors.Key(), id).
	orm.Table[Author, model.ID[Author]]
	// Name stores the author name. Example: orm.TextSet(Authors.Name, "Writer").
	Name orm.Text[Author, string]
	// Articles binds the eager collection. Example: AuthorArticles.Using(Articles.Query()).
	Articles orm.Many[Author, Article]
}

// Authors publishes the author declaration. Example: orm.Insert(ctx, tx, Authors.Table,orm.TextSet(Authors.Name, "Writer")).
var Authors = orm.Define("article_authors", func(b orm.Builder[Author], m *Author) AuthorTable {
	return AuthorTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name), orm.ManyOf(b, &m.Articles)}
})

// ArticleTable exposes stored fields and external slots. Example: Articles.Query().
type ArticleTable struct {
	// Table owns the concrete article key. Example: orm.KeyEq(Articles.Key(), id).
	orm.Table[Article, model.ID[Article]]
	// Slug stores the public slug. Example: orm.TextEq(Articles.Slug, "welcome").
	Slug orm.Text[Article, string]
	// AuthorID retains the author key type. Example: orm.ColumnSet(Articles.AuthorID, author.ID).
	AuthorID orm.Column[Article, model.ID[Author]]
	// DeletedAt stores soft deletion; Restore clears it. Example: Articles.DeletedAt.IsNull().
	DeletedAt orm.SoftDelete[Article, temporal.DateTime]
	// Soft authorizes physical deletion. Example: Articles.ForceDelete(ctx,tx,Articles.Soft,id).
	Soft orm.SoftDeletable[Article]
	// Title binds localized text. Example: ArticleSlots.Title.Relation(Articles.Title).
	Title orm.Slot[Article, translations.Text]
	// Summary binds localized summary text. Example: ArticleSlots.Summary.Relation(Articles.Summary).
	Summary orm.Slot[Article, translations.Text]
	// Logo binds a single file. Example: ArticleSlots.Logo.Relation(Articles.Logo).
	Logo orm.Slot[Article, attachments.One[Article]]
	// Galleries binds ordered files. Example: ArticleSlots.Galleries.Relation(Articles.Galleries).
	Galleries orm.Slot[Article, attachments.Many[Article]]
	// SEO binds the concrete metadata value. Example: ArticleSlots.SEO.Relation(Articles.SEO).
	SEO orm.Slot[Article, metadata.Value[SEO]]
	// Author binds the eager parent. Example: ArticleAuthor.Using(Authors.Query()).
	Author orm.One[Article, Author]
}

// Articles publishes the article declaration without changing the generated API. Example: Articles.Query().With(ArticleAuthor).
var Articles = orm.Define("articles", func(b orm.Builder[Article], m *Article) ArticleTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return ArticleTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Slug), orm.ColumnOf(b, &m.AuthorID), deleted, orm.SoftDeletes(deleted), orm.SlotOf(b, &m.Title), orm.SlotOf(b, &m.Summary), orm.SlotOf(b, &m.Logo), orm.SlotOf(b, &m.Galleries), orm.SlotOf(b, &m.SEO), orm.OneOf(b, &m.Author)}
})

// AuthorArticles loads the author's articles. Example: Authors.Query().With(AuthorArticles).
var AuthorArticles = orm.HasManyOf(Authors.Articles, orm.KeyExpr(Authors.Key()), orm.ColumnExpr(Articles.AuthorID))

// ArticleAuthor loads the article's author. Example: Articles.Query().With(ArticleAuthor).
var ArticleAuthor = orm.BelongsToOf(Articles.Author, orm.ColumnExpr(Articles.AuthorID), orm.KeyExpr(Authors.Key()))

// ArticleIdentity preserves the concrete stored identity. Example: ArticleIdentity.ReferenceOf(article).
var ArticleIdentity = orm.IdentityOf(Articles.Table, Articles.Key())

// AuthorIdentity preserves the concrete stored identity. Example: AuthorIdentity.ReferenceOf(author).
var AuthorIdentity = orm.IdentityOf(Authors.Table, Authors.Key())

// ArticleOwner pins the historical extension owner scope. Example: extensions.ORMIdentity(ArticleOwner).Reference(id).
var ArticleOwner = extensions.DefineORMOwner("articles", ArticleIdentity, extensions.OwnerOptions{StorageModel: "articles"})

// ArticleSlotHandles groups the thin external-store handles. Example: ArticleSlots.From(runtime).
type ArticleSlotHandles struct {
	// Title is the localized article heading shown to readers; it keeps its locale input and owner. Example: ArticleSlots.Title.Rule().
	Title translations.TextHandle[Article, model.ID[Article]]
	// Summary keeps its optional locale input. Example: ArticleSlots.Summary.MergeRule().
	Summary translations.TextHandle[Article, model.ID[Article]]
	// Logo keeps single-file capability. Example: ArticleSlots.Logo.Accepts(ctx,file).
	Logo attachments.OneHandle[Article, model.ID[Article]]
	// Galleries keeps multi-file capability. Example: ArticleSlots.Galleries.Accepts(ctx,file).
	Galleries attachments.ManyHandle[Article, model.ID[Article]]
	// SEO keeps its concrete value type. Example: ArticleSlots.SEO.Save(ctx,article,SEO{}).
	SEO metadata.ValueHandle[Article, model.ID[Article], SEO]
}

// ArticleSlots reuses the existing policies and stored names. Example: ArticleSlots.From(runtime).
var ArticleSlots = func() ArticleSlotHandles {
	return ArticleSlotHandles{
		Title:     translations.TextOf(ArticleOwner, "title", translations.Options{MaxBytes: 512, Require: i18n.DefaultLocale}, extensions.BindSlot(Articles.Key(), Articles.Title)),
		Summary:   translations.TextOf(ArticleOwner, "summary", translations.Options{}, extensions.BindSlot(Articles.Key(), Articles.Summary)),
		Logo:      attachments.OneOf(ArticleOwner, "logo", attachments.Policy{Disk: Files, Accepted: []storage.MediaType{"image/png"}, Image: value.Set(imaging.NewPlan().Fill(32, 32, false).Format(imaging.PNG))}, extensions.BindSlot(Articles.Key(), Articles.Logo)),
		Galleries: attachments.ManyOf(ArticleOwner, "galleries", attachments.Policy{Disk: Files, MaxFiles: 4, Accepted: []storage.MediaType{"image/png"}, Variants: []attachments.Variant{Thumbnail}}, extensions.BindSlot(Articles.Key(), Articles.Galleries)),
		SEO:       metadata.ValueOf(ArticleOwner, "seo", metadata.Spec[SEO]{}, SEOJSON(), extensions.BindSlot(Articles.Key(), Articles.SEO)),
	}
}()

// From borrows each runtime manager once. Example: bound:=ArticleSlots.From(runtime).
func (d ArticleSlotHandles) From(runtime slots.Runtime) ArticleSlotHandles {
	d.Title = d.Title.From(runtime.Translations)
	d.Summary = d.Summary.From(runtime.Translations)
	d.Logo = d.Logo.From(runtime.Attachments)
	d.Galleries = d.Galleries.From(runtime.Attachments)
	d.SEO = d.SEO.From(runtime.Metadata)
	return d
}

// All returns typed eager loaders. Example: Articles.Query().With(bound.All()...).
func (d ArticleSlotHandles) All() []orm.Relation[Article] {
	return []orm.Relation[Article]{d.Title.Relation(Articles.Title), d.Summary.Relation(Articles.Summary), d.Logo.Relation(Articles.Logo), d.Galleries.Relation(Articles.Galleries), d.SEO.Relation(Articles.SEO)}
}

// ORMExtensionDeclaration registers only the ORM cleanup observer family.
// Example: builder.Models(ORMExtensionDeclaration()).
func ORMExtensionDeclaration() slots.Declaration {
	return slots.DeclareORM(ArticleOwner, slots.Parts{
		Translations: []translations.Registration{ArticleSlots.Title.Registration(), ArticleSlots.Summary.Registration()},
		Attachments:  []attachments.Registration{ArticleSlots.Logo.Registration(), ArticleSlots.Galleries.Registration()},
		Metadata:     []metadata.Registration{ArticleSlots.SEO.Registration()},
		Slots:        []extensions.SlotDescription{ArticleSlots.Title.Describe(), ArticleSlots.Summary.Describe(), ArticleSlots.Logo.Describe(), ArticleSlots.Galleries.Describe(), ArticleSlots.SEO.Describe()},
	})
}

// Tables publishes both declarations. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Authors, Articles} }
