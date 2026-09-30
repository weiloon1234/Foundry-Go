package invalid

import (
	"context"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// An article slot writes only for an article.
func invalid(author articles.Author) {
	_ = articles.ArticleExtensions().Title.Save(context.Background(), author, map[i18n.LocaleID]string{"en": "x"})
}
