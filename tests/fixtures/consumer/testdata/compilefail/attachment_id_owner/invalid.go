package invalid

import (
	"context"
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(manager *attachments.Manager, profile profiles.Profile, id attachments.ID[articles.Author]) {
	_, _ = profiles.Avatar.Find(context.Background(), manager, profiles.ProfileIdentity.ReferenceOf(profile), id)
}
