package invalid

import (
	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

// A translated slot's policy entry cannot receive an attachment policy.
var _ = articles.ArticleExtensionSet{Title: attachments.Policy{}}
