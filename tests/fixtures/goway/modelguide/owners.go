package modelguide

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// doc-example:begin generated-slot-owner
var ArticleOwner = extensions.DefineORMOwner("guide-articles", articlesIdentity, extensions.OwnerOptions{StorageModel: "guide_articles"})
var ArticleTitle = translations.TextOf(ArticleOwner, "title", translations.Options{}, extensions.BindSlot(Articles.Key(), Articles.Title))
var ArticleCover = attachments.OneOf(ArticleOwner, "cover", attachments.Policy{
	Disk: storage.DefineDisk("public"), Accepted: []storage.MediaType{"image/png"},
}, extensions.BindSlot(Articles.Key(), Articles.Cover))

// doc-example:end generated-slot-owner
