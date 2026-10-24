package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

func invalid(author a.Author) {
	_ = a.ArticleExtensions.Title.Save(context.Background(), author, map[i18n.LocaleID]string{"en": "x"})
}
