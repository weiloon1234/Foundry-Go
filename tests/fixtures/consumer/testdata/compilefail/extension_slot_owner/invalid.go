package invalid

import (
	"context"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// An article slot keeps its owner type; another model's reference is rejected.
func invalid(manager *translations.Manager, author articles.Author) {
	_ = articles.ArticleExtensions().Title.Field().Set(context.Background(), manager, author.FoundryReference(), "en", "text")
}
