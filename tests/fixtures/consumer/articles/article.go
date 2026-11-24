// Package articles demonstrates model extension slots. The model declares
// translated text, attachments and typed metadata as ordinary fields with typed
// slot policies; Foundry owns their shared tables, loading and cleanup.
package articles

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Files stores article logos and galleries.
var Files = storage.DefineDisk("article-files")

// Thumbnail is a derived gallery image stored beside each original.
var Thumbnail = attachments.DefineVariant("thumbnail", imaging.NewPlan().Fit(16, 16, false).Format(imaging.PNG))

// SEO is schemaless-by-migration metadata: adding optional fields needs no
// migration; removing or renaming one requires a metadata version increment.
//
//foundry:dto
type SEO struct {
	Keywords  value.Optional[[]string] `json:"keywords,omitzero"`
	Canonical value.Optional[string]   `json:"canonical,omitzero"`
}

//foundry:model table=article_authors var=Authors key=ID identity=false
type Author struct {
	// Table owns the concrete author key. Example: orm.Eq(Authors.Key(), id).
	ID model.ID[Author]
	// Name stores the author name. Example: orm.Set(Authors.Name, "Writer").
	Name string
	// Articles binds the eager collection. Example: AuthorArticles.Using(Articles.Query()).
	Articles relation.Many[Article]
}

//foundry:model table=articles var=Articles key=ID identity=false
type Article struct {
	// Table owns the concrete article key. Example: orm.Eq(Articles.Key(), id).
	ID model.ID[Article]
	// Slug stores the public slug. Example: orm.Eq(Articles.Slug, "welcome").
	Slug string
	// AuthorID retains the author key type. Example: orm.Set(Articles.AuthorID, author.ID).
	AuthorID model.ID[Author] `foundry:"role=column"`
	// Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through typed assignments uses ordinary create/update hooks rather than deletion/restoration events.
	//
	// DeletedAt stores soft deletion; Restore clears it. Example: Articles.DeletedAt.IsNull().
	//
	// Soft authorizes physical deletion. Example: Articles.ForceDelete(ctx,tx,Articles.Soft,id).
	DeletedAt value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`

	// Extension slot, not a column: translated text stored in foundry_model_translations as field "title", one row per locale; policy: ArticleSlots.Title. Descriptor: ArticleSlots.Title; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Title.Relation(Articles.Title) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule(). Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "title" in ArticleSlots when renaming this Go field.
	// Title is the localized article heading shown to readers.
	//
	// Title binds localized text. Example: ArticleSlots.Title.Relation(Articles.Title).
	Title translations.Text
	// Extension slot, not a column: translated text stored in foundry_model_translations as field "summary", one row per locale; policy: ArticleSlots.Summary. Descriptor: ArticleSlots.Summary; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Summary.Relation(Articles.Summary) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule(). Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "summary" in ArticleSlots when renaming this Go field.
	//
	// Summary binds localized summary text. Example: ArticleSlots.Summary.Relation(Articles.Summary).
	Summary translations.Text
	// Extension slot, not a column: the single file of attachment collection "logo", recorded in foundry_attachments with its bytes on the policy's disk; policy: ArticleSlots.Logo. Descriptor: ArticleSlots.Logo; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Logo.Relation(Articles.Logo) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with ReplaceFile after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "logo" in ArticleSlots when renaming this Go field.
	//
	// Logo binds a single file. Example: ArticleSlots.Logo.Relation(Articles.Logo).
	Logo attachments.One[Article]
	// Extension slot, not a column: the ordered files of attachment collection "galleries", recorded in foundry_attachments with their bytes on the policy's disk; policy: ArticleSlots.Galleries. Descriptor: ArticleSlots.Galleries; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Galleries.Relation(Articles.Galleries) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with AddFile or attachments.AddFilesTo after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "galleries" in ArticleSlots when renaming this Go field.
	//
	// Galleries binds ordered files. Example: ArticleSlots.Galleries.Relation(Articles.Galleries).
	Galleries attachments.Many[Article]
	// Extension slot, not a column: the typed metadata value "seo" stored in foundry_model_metadata, with no model column; policy: ArticleSlots.SEO. Descriptor: ArticleSlots.SEO; bind the namespace once with ArticleSlots.From(runtime), then pass bound.SEO.Relation(Articles.SEO) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn in the model's transaction. Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "seo" in ArticleSlots when renaming this Go field.
	//
	// SEO binds the concrete metadata value. Example: ArticleSlots.SEO.Relation(Articles.SEO).
	SEO metadata.Value[SEO]

	// Author binds the eager parent. Example: ArticleAuthor.Using(Authors.Query()).
	Author relation.One[Author]
}
