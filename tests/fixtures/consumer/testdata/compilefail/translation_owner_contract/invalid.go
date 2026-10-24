package invalid

import (
	"context"
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(manager *translations.Manager, other articles.Author) {
	_ = profiles.Label.Set(context.Background(), manager, articles.AuthorIdentity.ReferenceOf(other), "en", "text")
}
