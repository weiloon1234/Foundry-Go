package invalid

import (
	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// Descriptors bind the complete runtime, never an individual manager.
func invalid(manager *translations.Manager) {
	_ = articles.ArticleExtensions().From(manager)
}
