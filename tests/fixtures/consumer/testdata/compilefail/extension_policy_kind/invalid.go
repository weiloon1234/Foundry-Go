package invalid

import (
	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// A translated slot cannot receive an attachment policy.
var _ = translations.TextOf(articles.ArticleOwner, "title", attachments.Policy{}, extensions.BindSlot(articles.Articles.Key(), articles.Articles.Title))
