package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(manager *attachments.Manager, profile profiles.Profile, id attachments.ID[models.Order]) {
	_, _ = profiles.Avatar.Find(context.Background(), manager, profile.FoundryReference(), id)
}
