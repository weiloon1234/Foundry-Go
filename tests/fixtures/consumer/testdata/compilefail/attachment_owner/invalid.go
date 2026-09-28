package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(manager *attachments.Manager, other models.Order) {
	_, _ = profiles.Avatar.Replace(context.Background(), manager, other.FoundryReference(), attachments.Upload{})
}
