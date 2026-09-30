// Package articles demonstrates model extension slots. The model declares
// translated text, attachments and typed metadata as ordinary fields with one
// policy method; Foundry owns their shared tables, loading and cleanup.
package articles

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/i18n"
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

//foundry:model table=article_authors
type Author struct {
	ID       model.ID[Author]
	Name     string
	Articles relation.Many[Article]
}

func (Author) DefineRelations() AuthorRelationSet {
	return AuthorRelationSet{Articles: query.HasMany(AuthorFields().ID, ArticleFields().AuthorID)}
}

//foundry:model table=articles
type Article struct {
	ID       model.ID[Article]
	Slug     string
	AuthorID model.ID[Author]
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]

	// Foundry field behavior (generated): Extension slot, not a column: translated text stored in foundry_model_translations as field "title", one row per locale; policy: [Article.DefineExtensions] entry Title. Descriptor: ArticleExtensions().Title; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule(). Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=title" pins it.
	Title translations.Text
	// Foundry field behavior (generated): Extension slot, not a column: translated text stored in foundry_model_translations as field "summary", one row per locale; policy: [Article.DefineExtensions] entry Summary. Descriptor: ArticleExtensions().Summary; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule(). Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=summary" pins it.
	Summary translations.Text
	// Foundry field behavior (generated): Extension slot, not a column: the single file of attachment collection "logo", recorded in foundry_attachments with its bytes on the policy's disk; policy: [Article.DefineExtensions] entry Logo. Descriptor: ArticleExtensions().Logo; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with ReplaceFile after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=logo" pins it.
	Logo attachments.One[Article]
	// Foundry field behavior (generated): Extension slot, not a column: the ordered files of attachment collection "galleries", recorded in foundry_attachments with their bytes on the policy's disk; policy: [Article.DefineExtensions] entry Galleries. Descriptor: ArticleExtensions().Galleries; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with AddFile or attachments.AddFiles after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=galleries" pins it.
	Galleries attachments.Many[Article]
	// Foundry field behavior (generated): Extension slot, not a column: the typed metadata value "seo" stored in foundry_model_metadata, with no model column; policy: [Article.DefineExtensions] entry SEO. Descriptor: ArticleExtensions().SEO; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn in the model's transaction. Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=seo" pins it.
	SEO metadata.Value[SEO]

	Author relation.One[Author]
}

func (Article) DefineExtensions() ArticleExtensionSet {
	return ArticleExtensionSet{
		Title: translations.Options{MaxBytes: 512, Require: i18n.DefaultLocale},
		Logo: attachments.Policy{
			Disk:     Files,
			Accepted: []storage.MediaType{"image/png"},
			Image:    value.Set(imaging.NewPlan().Fill(32, 32, false).Format(imaging.PNG)),
		},
		Galleries: attachments.Policy{
			Disk:     Files,
			MaxFiles: 4,
			Accepted: []storage.MediaType{"image/png"},
			Variants: []attachments.Variant{Thumbnail},
		},
	}
}

func (Article) DefineRelations() ArticleRelationSet {
	return ArticleRelationSet{Author: query.BelongsTo(ArticleFields().AuthorID, AuthorFields().ID)}
}
