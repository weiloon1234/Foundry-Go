package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.DefineField("slug", func(a.Article) string { return "" }).Rules(a.ArticleExtensions.Title.Rule())
