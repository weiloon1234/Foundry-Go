package invalid

import (
	"context"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// An article slot keeps its owner type; another model's reference is rejected.
func invalid(manager *translations.Manager, author articles.Author) {
	_ = translations.Define(articles.ArticleOwner, "title", translations.Options{}).Set(context.Background(), manager, articles.AuthorIdentity.ReferenceOf(author), "en", "text")
}
