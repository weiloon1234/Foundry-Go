package invalid

import (
	"context"
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/metadata"
)

func invalid(manager *metadata.Manager, other articles.Author) {
	_, _ = profiles.PreferencesKey.Get(context.Background(), manager, articles.AuthorIdentity.ReferenceOf(other))
}
