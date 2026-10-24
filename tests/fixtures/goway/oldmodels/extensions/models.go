// Package extensions is the generated baseline for the identical four-slot ORM workload.
package extensions

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/translations"
)

//foundry:model table=extension_articles primary=ID
type Article struct {
	ID int64
	// Foundry field behavior (generated): Extension slot, not a column: translated text stored in foundry_model_translations as field "title", one row per locale; policy: defineArticleExtensions entry Title. Descriptor: ArticleExtensions().Title; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule(). Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=title" pins it.
	Title translations.Text
	// Foundry field behavior (generated): Extension slot, not a column: the single file of attachment collection "cover", recorded in foundry_attachments with its bytes on the policy's disk; policy: defineArticleExtensions entry Cover. Descriptor: ArticleExtensions().Cover; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with ReplaceFile after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=cover" pins it.
	Cover attachments.One[Article]
	// Foundry field behavior (generated): Extension slot, not a column: the ordered files of attachment collection "gallery", recorded in foundry_attachments with their bytes on the policy's disk; policy: defineArticleExtensions entry Gallery. Descriptor: ArticleExtensions().Gallery; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Publish with AddFile or attachments.AddFiles after the model commits, and check uploads with Accepts. Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=gallery" pins it.
	Gallery attachments.Many[Article]
	// Foundry field behavior (generated): Extension slot, not a column: the typed metadata value "seo" stored in foundry_model_metadata, with no model column; policy: defineArticleExtensions entry SEO. Descriptor: ArticleExtensions().SEO; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. Write with SaveIn in the model's transaction. Hard deletion removes the data; soft deletion keeps it. Renaming this field changes its stored name unless foundry:"name=seo" pins it.
	SEO metadata.Value[string]
}

//foundry:model table=extension_authors primary=ID
type Author struct{ ID int64 }

func defineArticleExtensions() ArticleExtensionSet {
	return ArticleExtensionSet{Cover: attachments.Policy{Disk: storage.DefineDisk("local")}, Gallery: attachments.Policy{Disk: storage.DefineDisk("local")}}
}
