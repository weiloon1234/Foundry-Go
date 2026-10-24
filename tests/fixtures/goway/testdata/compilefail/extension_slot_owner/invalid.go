package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/translations"
)

var wrong orm.Slot[a.Author, translations.Text]
var _ = a.ArticleExtensions.Title.Relation(wrong)
