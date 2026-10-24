package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(m *translations.Manager) { _ = a.ArticleExtensions.From(m) }
