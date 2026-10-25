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

type Author struct {
	ID       model.ID[Author]
	Name     string
	Articles relation.Many[Article]
}

type Article struct {
	ID       model.ID[Article]
	Slug     string
	AuthorID model.ID[Author]
	// Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through typed assignments uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]

	// Extension slot, not a column: translated text stored in foundry_model_translations as field "title", one row per locale; policy: ArticleSlots.Title. Descriptor: ArticleSlots.Title; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Title.Relation(Articles.Title) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule(). Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "title" in ArticleSlots when renaming this Go field.
	// Title is the localized article heading shown to readers.
	Title translations.Text
	// Extension slot, not a column: translated text stored in foundry_model_translations as field "summary", one row per locale; policy: ArticleSlots.Summary. Descriptor: ArticleSlots.Summary; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Summary.Relation(Articles.Summary) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule(). Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "summary" in ArticleSlots when renaming this Go field.
	Summary translations.Text
	// Extension slot, not a column: the single file of attachment collection "logo", recorded in foundry_attachments with its bytes on the policy's disk; policy: ArticleSlots.Logo. Descriptor: ArticleSlots.Logo; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Logo.Relation(Articles.Logo) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with ReplaceFile after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "logo" in ArticleSlots when renaming this Go field.
	Logo attachments.One[Article]
	// Extension slot, not a column: the ordered files of attachment collection "galleries", recorded in foundry_attachments with their bytes on the policy's disk; policy: ArticleSlots.Galleries. Descriptor: ArticleSlots.Galleries; bind the namespace once with ArticleSlots.From(runtime), then pass bound.Galleries.Relation(Articles.Galleries) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with AddFile or attachments.AddFilesTo after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "galleries" in ArticleSlots when renaming this Go field.
	Galleries attachments.Many[Article]
	// Extension slot, not a column: the typed metadata value "seo" stored in foundry_model_metadata, with no model column; policy: ArticleSlots.SEO. Descriptor: ArticleSlots.SEO; bind the namespace once with ArticleSlots.From(runtime), then pass bound.SEO.Relation(Articles.SEO) to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn in the model's transaction. Hard deletion removes the data; soft deletion keeps it. Keep the explicit stored name "seo" in ArticleSlots when renaming this Go field.
	SEO metadata.Value[SEO]

	Author relation.One[Author]
}
