package invalid

import (
	"context"
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(manager *attachments.Manager, other articles.Author) {
	_, _ = profiles.Avatar.Replace(context.Background(), manager, articles.AuthorIdentity.ReferenceOf(other), attachments.Upload{})
}
